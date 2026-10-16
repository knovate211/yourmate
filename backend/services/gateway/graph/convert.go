package graph

import (
	"strings"
	"time"

	communityv1 "github.com/yourmate/backend/gen/community/v1"
	identityv1 "github.com/yourmate/backend/gen/identity/v1"
	marketplacev1 "github.com/yourmate/backend/gen/marketplace/v1"
	"github.com/yourmate/backend/services/gateway/graph/model"
)

// Services use lower_snake strings; GraphQL enums are UPPER_SNAKE.
func up[T ~string](s string) T   { return T(strings.ToUpper(s)) }
func down[T ~string](v T) string { return strings.ToLower(string(v)) }

func downAll[T ~string](vs []T) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = down(v)
	}
	return out
}

func upAll[T ~string](vs []string) []T {
	out := make([]T, len(vs))
	for i, v := range vs {
		out[i] = up[T](v)
	}
	return out
}

func unix(t int64) time.Time { return time.Unix(t, 0).UTC() }

func unixPtr(t int64) *time.Time {
	if t == 0 {
		return nil
	}
	u := unix(t)
	return &u
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func intOr(p *int, def int) int {
	if p == nil {
		return def
	}
	return *p
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func displayName(u *identityv1.User) string {
	name := strings.TrimSpace(u.FirstName)
	if last := strings.TrimSpace(u.LastName); last != "" {
		name += " " + strings.ToUpper(string([]rune(last)[0])) + "."
	}
	return name
}

func toUser(u *identityv1.User) *model.User {
	if u == nil {
		return nil
	}
	admin := false
	for _, r := range u.Roles {
		admin = admin || r == "admin"
	}
	return &model.User{
		ID:             u.Id,
		FirstName:      u.FirstName,
		Bio:            u.Bio,
		CampusVerified: u.CampusEmailVerified || u.IdVerified,
		IDVerified:     u.IdVerified,
		JoinedAt:       unix(u.CreatedAt),
		IsAdmin:        admin,
		Raw:            u,
	}
}

func toCampus(c *identityv1.Campus) *model.Campus {
	if c == nil {
		return nil
	}
	return &model.Campus{
		ID:              c.Id,
		Slug:            c.Slug,
		Name:            c.Name,
		Country:         c.Country,
		City:            c.City,
		Timezone:        c.Timezone,
		Currency:        c.Currency,
		EmailDomains:    nonNil(c.EmailDomains),
		Status:          up[model.CampusStatus](c.Status),
		MemberCount:     int(c.MemberCount),
		Website:         strPtr(c.Website),
		Source:          up[model.CampusSource](c.Source),
		DetailsVerified: c.DetailsVerified,
	}
}

func toSubject(s *marketplacev1.Subject) *model.Subject {
	return &model.Subject{ID: s.Id, Code: s.Code, Name: s.Name, Category: s.Category, CampusID: strPtr(s.CampusId)}
}

func money(minor int64, currency string) *model.Money {
	return &model.Money{AmountMinor: int(minor), Currency: currency}
}

func toProvider(p *marketplacev1.ProviderProfile) *model.Provider {
	return &model.Provider{
		ID:                p.Id,
		Kind:              up[model.ProviderKind](p.Kind),
		DisplayName:       p.DisplayName,
		Headline:          p.Headline,
		Bio:               p.Bio,
		Rate:              money(p.RateMinor, p.Currency),
		Modes:             upAll[model.SessionMode](p.Modes),
		Languages:         nonNil(p.Languages),
		Services:          guideServices(p),
		Status:            up[model.ProviderStatus](p.Status),
		RatingAvg:         p.RatingAvg,
		RatingCount:       int(p.RatingCount),
		SessionsCompleted: int(p.SessionsCompleted),
		CreatedAt:         unix(p.CreatedAt),
		Raw:               p,
	}
}

// Guides' services are GuideService values; a mentor's list holds interview
// types, exposed as interviewTypes instead.
func guideServices(p *marketplacev1.ProviderProfile) []model.GuideService {
	if p.Kind != "guide" {
		return []model.GuideService{}
	}
	return upAll[model.GuideService](p.Services)
}

func toFeedback(f *marketplacev1.InterviewFeedback) *model.InterviewFeedback {
	out := &model.InterviewFeedback{
		Verdict:      up[model.InterviewVerdict](f.Verdict),
		Strengths:    f.Strengths,
		Improvements: f.Improvements,
		Resources:    f.Resources,
		CreatedAt:    unix(f.CreatedAt),
		UpdatedAt:    unix(f.UpdatedAt),
		Scores:       make([]*model.FeedbackScore, len(f.Scores)),
	}
	for i, sc := range f.Scores {
		out.Scores[i] = &model.FeedbackScore{Criterion: up[string](sc.Criterion), Score: int(sc.Score)}
	}
	return out
}

func toResumeReview(r *marketplacev1.ResumeReview) *model.ResumeReview {
	out := &model.ResumeReview{
		ID:             r.Id,
		TargetRole:     r.TargetRole,
		Notes:          r.Notes,
		FileName:       r.FileName,
		Price:          money(r.PriceMinor, r.Currency),
		TurnaroundDays: int(r.TurnaroundDays),
		Status:         up[model.ResumeReviewStatus](r.Status),
		StatusReason:   r.StatusReason,
		DueAt:          unixPtr(r.DueAt),
		AcceptedAt:     unixPtr(r.AcceptedAt),
		DeliveredAt:    unixPtr(r.DeliveredAt),
		Summary:        r.Summary,
		Sections:       make([]*model.ResumeSectionNote, len(r.Sections)),
		Rated:          r.Reviewed,
		CreatedAt:      unix(r.CreatedAt),
		UpdatedAt:      unix(r.UpdatedAt),
		Raw:            r,
	}
	if r.OverallScore > 0 {
		score := int(r.OverallScore)
		out.OverallScore = &score
	}
	for i, n := range r.Sections {
		out.Sections[i] = &model.ResumeSectionNote{Section: up[string](n.Section), Rating: int(n.Rating), Comment: n.Comment}
	}
	return out
}

func toProviders(ps []*marketplacev1.ProviderProfile) []*model.Provider {
	out := make([]*model.Provider, len(ps))
	for i, p := range ps {
		out[i] = toProvider(p)
	}
	return out
}

func toBooking(b *marketplacev1.Booking) *model.Booking {
	var service *model.GuideService
	if b.Service != "" && b.Kind == "guide" {
		s := up[model.GuideService](b.Service)
		service = &s
	}
	return &model.Booking{
		ID:              b.Id,
		Kind:            up[model.ProviderKind](b.Kind),
		Service:         service,
		StartAt:         unix(b.StartAt),
		EndAt:           unix(b.EndAt),
		DurationMinutes: int((b.EndAt - b.StartAt) / 60),
		Mode:            up[model.SessionMode](b.Mode),
		Location:        b.Location,
		Price:           money(b.PriceMinor, b.Currency),
		Notes:           b.Notes,
		Status:          up[model.BookingStatus](b.Status),
		StatusReason:    b.StatusReason,
		Reviewed:        b.Reviewed,
		CreatedAt:       unix(b.CreatedAt),
		UpdatedAt:       unix(b.UpdatedAt),
		Raw:             b,
	}
}

func toBookings(bs []*marketplacev1.Booking) []*model.Booking {
	out := make([]*model.Booking, len(bs))
	for i, b := range bs {
		out[i] = toBooking(b)
	}
	return out
}

func toMessage(m *marketplacev1.Message) *model.Message {
	return &model.Message{ID: m.Id, Body: m.Body, CreatedAt: unix(m.CreatedAt), Raw: m}
}

func toReview(r *marketplacev1.Review) *model.Review {
	return &model.Review{ID: r.Id, Rating: int(r.Rating), Comment: r.Comment, Hidden: r.Hidden, CreatedAt: unix(r.CreatedAt), Raw: r}
}

func toVerification(v *identityv1.Verification) *model.Verification {
	out := &model.Verification{
		Status:     up[model.VerificationStatus](v.Status),
		Reason:     v.Reason,
		CreatedAt:  unixPtr(v.CreatedAt),
		ReviewedAt: unixPtr(v.ReviewedAt),
		Raw:        v,
	}
	if v.Id != "" {
		id := v.Id
		out.ID = &id
	}
	return out
}

func toPost(p *communityv1.Post) *model.Post {
	return &model.Post{
		ID:           p.Id,
		Type:         up[model.PostType](p.Type),
		Title:        p.Title,
		Body:         p.Body,
		EventAt:      unixPtr(p.EventAt),
		Location:     p.Location,
		Status:       up[model.PostStatus](p.Status),
		ReviewReason: p.ReviewReason,
		CreatedAt:    unix(p.CreatedAt),
		Raw:          p,
	}
}

func toPosts(ps []*communityv1.Post) []*model.Post {
	out := make([]*model.Post, len(ps))
	for i, p := range ps {
		out[i] = toPost(p)
	}
	return out
}

func toReport(r *communityv1.Report) *model.Report {
	return &model.Report{
		ID:             r.Id,
		TargetType:     up[model.ReportTarget](r.TargetType),
		TargetID:       r.TargetId,
		Reason:         up[model.ReportReason](r.Reason),
		Details:        r.Details,
		Status:         up[model.ReportStatus](r.Status),
		ResolutionNote: r.ResolutionNote,
		CreatedAt:      unix(r.CreatedAt),
		ResolvedAt:     unixPtr(r.ResolvedAt),
		Raw:            r,
	}
}

func toNotification(n *marketplacev1.Notification) *model.Notification {
	return &model.Notification{ID: n.Id, Type: n.Type, Title: n.Title, Body: n.Body, Link: n.Link, Read: n.Read, CreatedAt: unix(n.CreatedAt)}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
