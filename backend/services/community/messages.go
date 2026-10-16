package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	communityv1 "github.com/yourmate/backend/gen/community/v1"
	"github.com/yourmate/backend/pkg/db"
	"github.com/yourmate/backend/pkg/grpcx"
)

// Direct messages: two people may chat privately while they share at least
// one active community. The first message is a request; the recipient
// accepts it (replying also accepts) or ignores it. Blocks are checked by the
// gateway, which owns the block list's service client.

func orderedPair(a, b string) (string, string) {
	if a < b {
		return a, b
	}
	return b, a
}

func (s *server) sharesCommunity(ctx context.Context, a, b string) (bool, error) {
	var ok bool
	err := s.db.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM community_members m1
		JOIN community_members m2 ON m2.community_id = m1.community_id
		JOIN communities c ON c.id = m1.community_id AND c.status = 'active'
		WHERE m1.user_id::text = $1 AND m2.user_id::text = $2)`, a, b).Scan(&ok)
	return ok, err
}

// conversationColumns reads a conversation from the viewer's side; the
// viewer's id is argument $1.
const conversationColumns = `c.id,
	CASE WHEN c.user_a::text = $1 THEN c.user_b ELSE c.user_a END,
	c.status, c.requested_by, c.last_message_at,
	(SELECT count(*) FROM direct_messages m WHERE m.conversation_id = c.id AND m.sender_id::text <> $1
		AND m.created_at > CASE WHEN c.user_a::text = $1 THEN c.a_last_read_at ELSE c.b_last_read_at END),
	(SELECT count(*) FROM direct_messages m WHERE m.conversation_id = c.id AND m.sender_id = c.requested_by),
	lm.id, lm.sender_id, lm.body, lm.created_at`

const conversationFrom = ` FROM conversations c
	LEFT JOIN LATERAL (SELECT m.id, m.sender_id, m.body, m.created_at FROM direct_messages m
		WHERE m.conversation_id = c.id ORDER BY m.created_at DESC LIMIT 1) lm ON true`

type conversationRow struct {
	c            *communityv1.Conversation
	requestCount int
}

func scanConversation(row pgx.Row) (*conversationRow, error) {
	c := &communityv1.Conversation{}
	var updated time.Time
	var unread, requestMsgs int
	var lmID, lmSender, lmBody *string
	var lmAt *time.Time
	if err := row.Scan(&c.Id, &c.OtherUserId, &c.Status, &c.RequestedBy, &updated, &unread, &requestMsgs,
		&lmID, &lmSender, &lmBody, &lmAt); err != nil {
		return nil, err
	}
	c.UpdatedAt = updated.Unix()
	c.UnreadCount = int32(unread)
	if lmID != nil {
		c.LastMessage = &communityv1.DirectMessage{Id: *lmID, ConversationId: c.Id, SenderId: *lmSender, Body: *lmBody, CreatedAt: lmAt.Unix()}
	}
	return &conversationRow{c: c, requestCount: requestMsgs}, nil
}

// finish fills in what depends on the viewer: whether they share a community
// and may send now. An ignored request reads "request" to both people: the
// sender just sees no answer, and the recipient can still accept it later.
func (s *server) finish(ctx context.Context, a *communityv1.Actor, r *conversationRow) (*communityv1.Conversation, error) {
	c := r.c
	shares, err := s.sharesCommunity(ctx, a.Id, c.OtherUserId)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	c.SharesCommunity = shares
	isRequester := c.RequestedBy == a.Id
	switch c.Status {
	case "active":
		c.CanSend = shares
	case "request":
		c.CanSend = shares && (!isRequester || r.requestCount < dmPendingLimit)
	case "declined":
		c.Status = "request"
		c.CanSend = false
	}
	return c, nil
}

func (s *server) loadConversation(ctx context.Context, a *communityv1.Actor, id string) (*conversationRow, error) {
	if err := requireUser(a); err != nil {
		return nil, err
	}
	r, err := scanConversation(s.db.QueryRow(ctx, `SELECT `+conversationColumns+conversationFrom+`
		WHERE c.id::text = $2 AND (c.user_a::text = $1 OR c.user_b::text = $1)`, a.Id, id))
	if err != nil {
		return nil, notFound(err, "conversation")
	}
	return r, nil
}

func (s *server) CanMessage(ctx context.Context, req *communityv1.UserPairRequest) (*communityv1.CanMessageResponse, error) {
	a := actorOf(req.Actor)
	if err := requireUser(a); err != nil {
		return nil, err
	}
	out := &communityv1.CanMessageResponse{}
	if req.UserId == "" || req.UserId == a.Id {
		return out, nil
	}
	shares, err := s.sharesCommunity(ctx, a.Id, req.UserId)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	out.Allowed = shares
	ua, ub := orderedPair(a.Id, req.UserId)
	if err := s.db.QueryRow(ctx, `SELECT id FROM conversations WHERE user_a::text = $1 AND user_b::text = $2`, ua, ub).Scan(&out.ConversationId); err != nil && !db.IsNoRows(err) {
		return nil, notFound(err, "user")
	}
	return out, nil
}

func (s *server) StartConversation(ctx context.Context, req *communityv1.UserPairRequest) (*communityv1.Conversation, error) {
	a := actorOf(req.Actor)
	if err := requireUser(a); err != nil {
		return nil, err
	}
	if req.UserId == "" || req.UserId == a.Id {
		return nil, grpcx.Invalid("choose someone else to message")
	}
	shares, err := s.sharesCommunity(ctx, a.Id, req.UserId)
	if err != nil {
		return nil, notFound(err, "user")
	}
	if !shares {
		return nil, grpcx.Denied("NO_SHARED_COMMUNITY: you can message people who are in a community with you")
	}
	ua, ub := orderedPair(a.Id, req.UserId)
	var id string
	err = s.db.QueryRow(ctx, `SELECT id FROM conversations WHERE user_a::text = $1 AND user_b::text = $2`, ua, ub).Scan(&id)
	if db.IsNoRows(err) {
		var recent int
		if err := s.db.QueryRow(ctx, `SELECT count(*) FROM conversations WHERE requested_by::text = $1 AND created_at > now() - interval '1 hour'`, a.Id).Scan(&recent); err != nil {
			return nil, grpcx.Internal(err)
		}
		if recent >= dmNewPerHour {
			return nil, grpcx.TooMany("you've started a lot of new chats this hour; try again later")
		}
		err = s.db.QueryRow(ctx, `INSERT INTO conversations (user_a, user_b, requested_by) VALUES ($1, $2, $3)
			ON CONFLICT (user_a, user_b) DO UPDATE SET user_a = EXCLUDED.user_a RETURNING id`, ua, ub, a.Id).Scan(&id)
	}
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	r, err := s.loadConversation(ctx, a, id)
	if err != nil {
		return nil, err
	}
	return s.finish(ctx, a, r)
}

func (s *server) ListConversations(ctx context.Context, req *communityv1.ListConversationsRequest) (*communityv1.ListConversationsResponse, error) {
	a := actorOf(req.Actor)
	if err := requireUser(a); err != nil {
		return nil, err
	}
	limit, offset := db.Page(req.Page, req.PageSize, 30, 100)
	// Requests are messages others started that the viewer hasn't answered;
	// the inbox is everything else the viewer is part of. Empty chats (opened
	// but never written in) are left out of both.
	cond := `(c.user_a::text = $1 OR c.user_b::text = $1) AND EXISTS (SELECT 1 FROM direct_messages m WHERE m.conversation_id = c.id) AND `
	if req.Folder == "requests" {
		cond += `c.status = 'request' AND c.requested_by::text <> $1`
	} else {
		cond += `(c.status = 'active' OR c.requested_by::text = $1)`
	}
	out := &communityv1.ListConversationsResponse{}
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM conversations c WHERE `+cond, a.Id).Scan(&out.Total); err != nil {
		return nil, grpcx.Internal(err)
	}
	rows, err := s.db.Query(ctx, `SELECT `+conversationColumns+conversationFrom+` WHERE `+cond+`
		ORDER BY c.last_message_at DESC LIMIT $2 OFFSET $3`, a.Id, limit, offset)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	var list []*conversationRow
	for rows.Next() {
		r, err := scanConversation(rows)
		if err != nil {
			rows.Close()
			return nil, grpcx.Internal(err)
		}
		list = append(list, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, grpcx.Internal(err)
	}
	for _, r := range list {
		c, err := s.finish(ctx, a, r)
		if err != nil {
			return nil, err
		}
		out.Conversations = append(out.Conversations, c)
	}
	return out, nil
}

func (s *server) GetConversation(ctx context.Context, req *communityv1.ConversationRef) (*communityv1.Conversation, error) {
	a := actorOf(req.Actor)
	r, err := s.loadConversation(ctx, a, req.ConversationId)
	if err != nil {
		return nil, err
	}
	return s.finish(ctx, a, r)
}

func (s *server) RespondToConversation(ctx context.Context, req *communityv1.RespondToConversationRequest) (*communityv1.Conversation, error) {
	a := actorOf(req.Actor)
	r, err := s.loadConversation(ctx, a, req.ConversationId)
	if err != nil {
		return nil, err
	}
	if r.c.RequestedBy == a.Id {
		return nil, grpcx.Denied("only the person you messaged can accept the request")
	}
	if r.c.Status == "active" {
		return nil, grpcx.Precondition("this chat isn't waiting for an answer")
	}
	status := "declined"
	if req.Accept {
		status = "active"
	}
	if _, err := s.db.Exec(ctx, `UPDATE conversations SET status = $2 WHERE id = $1`, r.c.Id, status); err != nil {
		return nil, grpcx.Internal(err)
	}
	return s.GetConversation(ctx, &communityv1.ConversationRef{Actor: a, ConversationId: r.c.Id})
}

func (s *server) ListDirectMessages(ctx context.Context, req *communityv1.ListDirectMessagesRequest) (*communityv1.ListDirectMessagesResponse, error) {
	a := actorOf(req.Actor)
	r, err := s.loadConversation(ctx, a, req.ConversationId)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `SELECT id, conversation_id, sender_id, body, created_at FROM direct_messages
		WHERE conversation_id = $1 AND created_at > $2 ORDER BY created_at LIMIT 500`, r.c.Id, time.Unix(req.After, 0))
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer rows.Close()
	out := &communityv1.ListDirectMessagesResponse{}
	for rows.Next() {
		m := &communityv1.DirectMessage{}
		var created time.Time
		if err := rows.Scan(&m.Id, &m.ConversationId, &m.SenderId, &m.Body, &created); err != nil {
			return nil, grpcx.Internal(err)
		}
		m.CreatedAt = created.Unix()
		out.Messages = append(out.Messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, grpcx.Internal(err)
	}
	// Reading the thread marks it read.
	if _, err := s.db.Exec(ctx, `UPDATE conversations SET
		a_last_read_at = CASE WHEN user_a::text = $2 THEN now() ELSE a_last_read_at END,
		b_last_read_at = CASE WHEN user_b::text = $2 THEN now() ELSE b_last_read_at END
		WHERE id = $1`, r.c.Id, a.Id); err != nil {
		return nil, grpcx.Internal(err)
	}
	return out, nil
}

func (s *server) SendDirectMessage(ctx context.Context, req *communityv1.SendDirectMessageRequest) (*communityv1.SendDirectMessageResponse, error) {
	a := actorOf(req.Actor)
	body := strings.TrimSpace(req.Body)
	if body == "" || len(body) > 2000 {
		return nil, grpcx.Invalid("messages must be 1–2000 characters")
	}
	r, err := s.loadConversation(ctx, a, req.ConversationId)
	if err != nil {
		return nil, err
	}
	c, err := s.finish(ctx, a, r)
	if err != nil {
		return nil, err
	}
	isRequester := c.RequestedBy == a.Id
	switch {
	case !c.SharesCommunity:
		return nil, grpcx.Precondition("NO_SHARED_COMMUNITY: you no longer share a community, so this chat is read-only")
	case r.c.Status == "declined":
		return nil, grpcx.Precondition("they haven't accepted your message request")
	case r.c.Status == "request" && isRequester && r.requestCount >= dmPendingLimit:
		return nil, grpcx.TooMany("wait for them to accept your message request before sending more")
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	// Was the recipient already holding an unread message from this chat?
	var unreadBefore int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM direct_messages m JOIN conversations c ON c.id = m.conversation_id
		WHERE c.id = $1 AND m.sender_id::text = $2
		AND m.created_at > CASE WHEN c.user_a::text = $2 THEN c.b_last_read_at ELSE c.a_last_read_at END`, c.Id, a.Id).Scan(&unreadBefore); err != nil {
		return nil, grpcx.Internal(err)
	}
	m := &communityv1.DirectMessage{}
	var created time.Time
	if err := tx.QueryRow(ctx, `INSERT INTO direct_messages (conversation_id, sender_id, body) VALUES ($1, $2, $3)
		RETURNING id, conversation_id, sender_id, body, created_at`, c.Id, a.Id, body).Scan(&m.Id, &m.ConversationId, &m.SenderId, &m.Body, &created); err != nil {
		return nil, grpcx.Internal(err)
	}
	m.CreatedAt = created.Unix()
	status := r.c.Status
	if status == "request" && !isRequester {
		status = "active" // replying to a request accepts it
	}
	if _, err := tx.Exec(ctx, `UPDATE conversations SET status = $3, last_message_at = $4,
		a_last_read_at = CASE WHEN user_a::text = $2 THEN $4 ELSE a_last_read_at END,
		b_last_read_at = CASE WHEN user_b::text = $2 THEN $4 ELSE b_last_read_at END
		WHERE id = $1`, c.Id, a.Id, status, created); err != nil {
		return nil, grpcx.Internal(err)
	}
	// Delivered to live streams once the transaction commits.
	notice, _ := json.Marshal(dmNotice{MessageID: m.Id, Users: []string{a.Id, c.OtherUserId}})
	if _, err := tx.Exec(ctx, `SELECT pg_notify($1, $2)`, dmChannel, string(notice)); err != nil {
		return nil, grpcx.Internal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, grpcx.Internal(err)
	}
	return &communityv1.SendDirectMessageResponse{Message: m, RecipientId: c.OtherUserId, Notify: unreadBefore == 0, IsRequest: status == "request"}, nil
}

func (s *server) InboxSummary(ctx context.Context, a *communityv1.Actor) (*communityv1.InboxSummaryResponse, error) {
	if err := requireUser(a); err != nil {
		return nil, err
	}
	out := &communityv1.InboxSummaryResponse{}
	var unread, requests int
	err := s.db.QueryRow(ctx, fmt.Sprintf(`SELECT
		count(*) FILTER (WHERE (c.status = 'active' OR c.requested_by::text = $1) AND %[1]s > 0),
		count(*) FILTER (WHERE c.status = 'request' AND c.requested_by::text <> $1
			AND EXISTS (SELECT 1 FROM direct_messages m WHERE m.conversation_id = c.id))
		FROM conversations c WHERE c.user_a::text = $1 OR c.user_b::text = $1`,
		`(SELECT count(*) FROM direct_messages m WHERE m.conversation_id = c.id AND m.sender_id::text <> $1
			AND m.created_at > CASE WHEN c.user_a::text = $1 THEN c.a_last_read_at ELSE c.b_last_read_at END)`), a.Id).Scan(&unread, &requests)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	out.UnreadConversations, out.Requests = int32(unread), int32(requests)
	return out, nil
}
