package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	communityv1 "github.com/yourmate/backend/gen/community/v1"
	"github.com/yourmate/backend/pkg/grpcx"
)

// Live message delivery. Sending a message issues pg_notify inside the same
// transaction, so the event fires only once the message is committed. Every
// instance of this service LISTENs on the channel and pushes the event to the
// streams open on that instance, so delivery works with any number of
// instances behind any number of gateways.

const dmChannel = "direct_messages"

// The notification carries ids only: Postgres caps payloads at 8000 bytes and
// a message body may be longer.
type dmNotice struct {
	MessageID string   `json:"m"`
	Users     []string `json:"u"` // sender and recipient
}

type hub struct {
	mu   sync.Mutex
	subs map[string]map[chan *communityv1.MessageEvent]struct{}
}

func newHub() *hub { return &hub{subs: map[string]map[chan *communityv1.MessageEvent]struct{}{}} }

func (h *hub) subscribe(userID string) chan *communityv1.MessageEvent {
	ch := make(chan *communityv1.MessageEvent, 32)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subs[userID] == nil {
		h.subs[userID] = map[chan *communityv1.MessageEvent]struct{}{}
	}
	h.subs[userID][ch] = struct{}{}
	return ch
}

func (h *hub) unsubscribe(userID string, ch chan *communityv1.MessageEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.subs[userID], ch)
	if len(h.subs[userID]) == 0 {
		delete(h.subs, userID)
	}
}

func (h *hub) has(userID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs[userID]) > 0
}

// publish never blocks: a stream that has fallen behind misses the event and
// the client's periodic refresh catches it up.
func (h *hub) publish(userID string, ev *communityv1.MessageEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[userID] {
		select {
		case ch <- ev:
		default:
		}
	}
}

// listen keeps a dedicated connection LISTENing, reconnecting on failure.
func (s *server) listen(ctx context.Context, pool *pgxpool.Pool) {
	for backoff := time.Second; ctx.Err() == nil; {
		if err := s.listenOnce(ctx, pool); err != nil && ctx.Err() == nil {
			slog.Warn("message listener stopped; reconnecting", "err", err, "in", backoff)
			time.Sleep(backoff)
			if backoff < 30*time.Second {
				backoff *= 2
			}
		}
	}
}

func (s *server) listenOnce(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "LISTEN "+dmChannel); err != nil {
		return err
	}
	slog.Info("listening for messages")
	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}
		var notice dmNotice
		if err := json.Unmarshal([]byte(n.Payload), &notice); err != nil {
			continue
		}
		var watched []string
		for _, u := range notice.Users {
			if s.hub.has(u) {
				watched = append(watched, u)
			}
		}
		if len(watched) == 0 {
			continue // nobody connected to this instance cares
		}
		m := &communityv1.DirectMessage{}
		var created time.Time
		if err := s.db.QueryRow(ctx, `SELECT id, conversation_id, sender_id, body, created_at FROM direct_messages WHERE id = $1`,
			notice.MessageID).Scan(&m.Id, &m.ConversationId, &m.SenderId, &m.Body, &created); err != nil {
			continue
		}
		m.CreatedAt = created.Unix()
		ev := &communityv1.MessageEvent{ConversationId: m.ConversationId, Message: m}
		for _, u := range watched {
			s.hub.publish(u, ev)
		}
	}
}

func (s *server) StreamMessageEvents(a *communityv1.Actor, stream communityv1.CommunityService_StreamMessageEventsServer) error {
	if err := requireUser(a); err != nil {
		return err
	}
	ch := s.hub.subscribe(a.Id)
	defer s.hub.unsubscribe(a.Id, ch)
	for {
		select {
		case <-stream.Context().Done():
			return nil
		case ev := <-ch:
			if err := stream.Send(ev); err != nil {
				return grpcx.Internal(err)
			}
		}
	}
}
