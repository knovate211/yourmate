package graph

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	communityv1 "github.com/yourmate/backend/gen/community/v1"
	identityv1 "github.com/yourmate/backend/gen/identity/v1"
	marketplacev1 "github.com/yourmate/backend/gen/marketplace/v1"
	"github.com/yourmate/backend/services/gateway/graph/model"
)

func (r *Resolver) user(ctx context.Context, id string) (*model.User, error) {
	u, ok, err := loadersFrom(ctx).users.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("user %s missing", id)
	}
	return toUser(u), nil
}

func (r *Resolver) campus(ctx context.Context, id string) (*model.Campus, error) {
	if id == "" {
		return nil, nil
	}
	c, ok, err := loadersFrom(ctx).campuses.load(ctx, id)
	if err != nil || !ok {
		return nil, err
	}
	return toCampus(c), nil
}

func toCampuses(cs []*identityv1.Campus) []*model.Campus {
	out := make([]*model.Campus, len(cs))
	for i, c := range cs {
		out[i] = toCampus(c)
	}
	return out
}

func (r *Resolver) campusPage(ctx context.Context, req *identityv1.ListCampusesRequest) (*model.CampusPage, error) {
	res, err := r.Identity.ListCampuses(ctx, req)
	if err != nil {
		return nil, err
	}
	for _, c := range res.Campuses {
		loadersFrom(ctx).campuses.prime(c.Id, c)
	}
	page := req.Page
	if page < 1 {
		page = 1
	}
	return &model.CampusPage{Items: toCampuses(res.Campuses), Total: int(res.Total), Page: int(page), PageSize: int(req.PageSize)}, nil
}

// session sets the refresh cookie and returns the access token to the client.
func (r *Resolver) session(ctx context.Context, res *identityv1.AuthResponse) *model.AuthPayload {
	r.setRefreshCookie(ctx, res.RefreshToken, res.RefreshExpiresAt)
	loadersFrom(ctx).users.prime(res.User.Id, res.User)
	return &model.AuthPayload{AccessToken: res.AccessToken, ExpiresAt: unix(res.AccessExpiresAt), User: toUser(res.User)}
}

// syncCard copies the public name/avatar onto the user's provider profiles.
func (r *Resolver) syncCard(ctx context.Context, u *identityv1.User) {
	if _, err := r.Market.SyncUserCard(ctx, &marketplacev1.SyncUserCardRequest{
		UserId: u.Id, DisplayName: displayName(u), AvatarKey: u.AvatarKey}); err != nil {
		slog.Warn("sync user card", "err", err)
	}
}

func confirmed(b *marketplacev1.Booking) bool {
	return b.Status == "accepted" || b.Status == "completed"
}

func isParty(ctx context.Context, b *marketplacev1.Booking) bool {
	id := viewerID(ctx)
	return id != "" && (id == b.StudentId || id == b.ProviderId)
}

func canSeePrivate(ctx context.Context, u *model.User) bool {
	v := viewer(ctx)
	return v != nil && (v.ID == u.ID || v.Admin)
}

func private(ctx context.Context, u *model.User, value string) *string {
	if !canSeePrivate(ctx, u) {
		return nil
	}
	return &value
}

func privateBool(ctx context.Context, u *model.User, value bool) *bool {
	if !canSeePrivate(ctx, u) {
		return nil
	}
	return &value
}

func isForbiddenCode(err error) bool { return status.Code(err) == codes.PermissionDenied }

func toAvailability(res *marketplacev1.AvailabilityResponse) *model.Availability {
	out := &model.Availability{Timezone: res.Timezone, Windows: make([]*model.AvailabilityWindow, len(res.Windows))}
	for i, w := range res.Windows {
		out.Windows[i] = &model.AvailabilityWindow{Weekday: int(w.Weekday), StartMinute: int(w.StartMinute), EndMinute: int(w.EndMinute)}
	}
	return out
}

func (r *Resolver) searchProviders(ctx context.Context, req *marketplacev1.SearchProvidersRequest) (*model.ProviderPage, error) {
	res, err := r.Market.SearchProviders(ctx, req)
	if err != nil {
		return nil, err
	}
	for _, p := range res.Profiles {
		loadersFrom(ctx).profiles.prime(p.Id, p)
	}
	page, size := req.Page, req.PageSize
	if page < 1 {
		page = 1
	}
	return &model.ProviderPage{Items: toProviders(res.Profiles), Total: int(res.Total), Page: int(page), PageSize: int(size)}, nil
}

func (r *Resolver) listPosts(ctx context.Context, req *communityv1.ListPostsRequest) (*model.PostPage, error) {
	res, err := r.CommunitySvc.ListPosts(ctx, req)
	if err != nil {
		return nil, err
	}
	return &model.PostPage{Items: toPosts(res.Posts), Total: int(res.Total)}, nil
}

// describeTarget gives admins a readable label and a link for a report's
// target. Targets may have been deleted since, which is not an error.
func (r *Resolver) describeTarget(ctx context.Context, rep *communityv1.Report) (label, link string) {
	gone := "(no longer available)"
	switch rep.TargetType {
	case "user":
		u, ok, err := loadersFrom(ctx).users.load(ctx, rep.TargetId)
		if err != nil || !ok {
			return gone, ""
		}
		return fmt.Sprintf("%s %s <%s>", u.FirstName, u.LastName, u.Email), "/admin/users?q=" + u.Email
	case "provider":
		p, ok, err := loadersFrom(ctx).profiles.load(ctx, rep.TargetId)
		if err != nil || !ok {
			return gone, ""
		}
		return fmt.Sprintf("%s (%s): %s", p.DisplayName, p.Kind, p.Headline), "/" + p.Kind + "s/" + p.Id
	case "review":
		rv, err := r.Market.GetReview(ctx, &marketplacev1.GetReviewRequest{Id: rep.TargetId})
		if err != nil {
			return gone, ""
		}
		link := ""
		if p, ok, err := loadersFrom(ctx).profiles.load(ctx, rv.ProviderProfileId); err == nil && ok {
			link = "/" + p.Kind + "s/" + p.Id
		}
		return fmt.Sprintf("%s %s", strings.Repeat("★", int(rv.Rating)), truncate(rv.Comment, 100)), link
	case "post":
		p, err := r.CommunitySvc.GetPost(ctx, &communityv1.GetPostRequest{Id: rep.TargetId})
		if err != nil {
			return gone, ""
		}
		return fmt.Sprintf("%s post: %s", p.Type, p.Title), "/feed"
	case "community_post":
		admin := &communityv1.Actor{IsAdmin: true}
		p, err := r.CommunitySvc.GetCommunityPost(ctx, &communityv1.PostRef{Actor: admin, PostId: rep.TargetId})
		if err != nil {
			return gone, ""
		}
		c, err := r.CommunitySvc.GetCommunity(ctx, &communityv1.GetCommunityRequest{Actor: admin, Id: p.CommunityId})
		if err != nil {
			return gone, ""
		}
		return fmt.Sprintf("Post in %s: %s", c.Name, truncate(p.Body, 100)), communityLink(c.Slug) + "/posts/" + p.Id
	case "community":
		c, err := r.CommunitySvc.GetCommunity(ctx, &communityv1.GetCommunityRequest{Actor: &communityv1.Actor{IsAdmin: true}, Id: rep.TargetId})
		if err != nil {
			return gone, ""
		}
		return "Community: " + c.Name, communityLink(c.Slug)
	case "booking":
		b, err := r.Market.GetBooking(ctx, &marketplacev1.GetBookingRequest{Id: rep.TargetId, ActorIsAdmin: true})
		if err != nil {
			return gone, ""
		}
		return fmt.Sprintf("%s session on %s (%s)", b.Kind, unix(b.StartAt).Format("2 Jan 2006 15:04 MST"), b.Status), "/bookings/" + b.Id
	}
	return gone, ""
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
