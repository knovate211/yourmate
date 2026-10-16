package graph

import (
	"context"
	"fmt"
	"strings"

	communityv1 "github.com/yourmate/backend/gen/community/v1"
	marketplacev1 "github.com/yourmate/backend/gen/marketplace/v1"
	"github.com/yourmate/backend/services/gateway/graph/model"
)

// actor describes the viewer to the community service: who they are, their
// campus, whether they're verified there, and whether they're an admin.
func (r *Resolver) actor(ctx context.Context) (*communityv1.Actor, error) {
	v := viewer(ctx)
	if v == nil {
		return &communityv1.Actor{}, nil
	}
	u, ok, err := loadersFrom(ctx).users.load(ctx, v.ID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return &communityv1.Actor{}, nil
	}
	return &communityv1.Actor{Id: u.Id, CampusId: u.CampusId, CampusVerified: u.CampusEmailVerified || u.IdVerified, IsAdmin: v.Admin}, nil
}

func toCommunity(c *communityv1.Community) *model.Community {
	return &model.Community{
		ID:          c.Id,
		Slug:        c.Slug,
		Name:        c.Name,
		Description: c.Description,
		Kind:        up[model.CommunityKind](c.Kind),
		JoinPolicy:  up[model.JoinPolicy](c.JoinPolicy),
		Rules:       c.Rules,
		IsDefault:   c.IsDefault,
		MemberCount: int(c.MemberCount),
		PostCount:   int(c.PostCount),
		CreatedAt:   unix(c.CreatedAt),
		Raw:         c,
	}
}

func toCommunityPost(p *communityv1.CommunityPost) *model.CommunityPost {
	return &model.CommunityPost{
		ID: p.Id, Body: p.Body, Pinned: p.Pinned, LikeCount: int(p.LikeCount), ReplyCount: int(p.ReplyCount),
		LikedByMe: p.ViewerLiked, CreatedAt: unix(p.CreatedAt), Raw: p,
	}
}

func toCommunityReply(rp *communityv1.Reply) *model.CommunityReply {
	return &model.CommunityReply{ID: rp.Id, Body: rp.Body, CreatedAt: unix(rp.CreatedAt), Raw: rp}
}

func communityRole(role string) model.CommunityRole {
	if role == "" {
		return model.CommunityRoleNone
	}
	return up[model.CommunityRole](role)
}

// communityByID is cached per request: a page of posts all share one community.
func (r *Resolver) communityByID(ctx context.Context, id string) (*communityv1.Community, error) {
	c, ok, err := loadersFrom(ctx).communities.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("community %s missing", id)
	}
	return c, nil
}

// hiddenAuthors are the people the viewer blocked; their posts are hidden.
func (r *Resolver) hiddenAuthors(ctx context.Context) []string {
	if viewerID(ctx) == "" {
		return nil
	}
	res, err := r.Market.ListBlocks(ctx, &marketplacev1.UserRequest{UserId: viewerID(ctx)})
	if err != nil {
		return nil
	}
	return res.UserIds
}

func (r *Resolver) notify(ctx context.Context, userID, typ, title, body, link string) {
	if userID == "" {
		return
	}
	_, _ = r.Market.Notify(ctx, &marketplacev1.NotifyRequest{UserId: userID, Type: typ, Title: title, Body: body, Link: link})
}

func communityLink(slug string) string { return "/communities/" + slug }

func preview(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	return truncate(s, n)
}

// Catalog values are GraphQL enum names, the services' values uppercased.
func marketOptions(opts []*marketplacev1.Option) []*model.Option {
	out := make([]*model.Option, len(opts))
	for i, o := range opts {
		out[i] = &model.Option{Value: strings.ToUpper(o.Value), Label: o.Label}
	}
	return out
}

func communityOptions(opts []*communityv1.Option) []*model.Option {
	out := make([]*model.Option, len(opts))
	for i, o := range opts {
		out[i] = &model.Option{Value: strings.ToUpper(o.Value), Label: o.Label}
	}
	return out
}

// blockedBetween reports whether either person blocked the other.
func (r *Resolver) blockedBetween(ctx context.Context, a, b string) bool {
	for _, pair := range [][2]string{{a, b}, {b, a}} {
		res, err := r.Market.ListBlocks(ctx, &marketplacev1.UserRequest{UserId: pair[0]})
		if err != nil {
			continue
		}
		for _, id := range res.UserIds {
			if id == pair[1] {
				return true
			}
		}
	}
	return false
}

func toConversation(c *communityv1.Conversation) *model.Conversation {
	return &model.Conversation{
		ID:              c.Id,
		Status:          up[model.ConversationStatus](c.Status),
		CanSend:         c.CanSend,
		SharesCommunity: c.SharesCommunity,
		UnreadCount:     int(c.UnreadCount),
		UpdatedAt:       unix(c.UpdatedAt),
		Raw:             c,
	}
}

func toDirectMessage(m *communityv1.DirectMessage) *model.DirectMessage {
	return &model.DirectMessage{ID: m.Id, Body: m.Body, CreatedAt: unix(m.CreatedAt), Raw: m}
}
