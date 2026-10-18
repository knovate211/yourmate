package graph

import (
	"context"
	"encoding/base64"
	"time"

	communityv1 "github.com/yourmate/backend/gen/community/v1"
	marketplacev1 "github.com/yourmate/backend/gen/marketplace/v1"
	"github.com/yourmate/backend/services/gateway/graph/model"
)

func toRoom(r *marketplacev1.Room, viewerIsProvider bool) *model.InterviewRoom {
	now := time.Now().Unix()
	out := &model.InterviewRoom{
		Problem:          r.Problem,
		Language:         up[string](r.Language),
		Code:             r.Code,
		Whiteboard:       r.Whiteboard,
		DraftScores:      toScores(r.DraftScores),
		OpensAt:          unix(r.OpensAt),
		ClosesAt:         unix(r.ClosesAt),
		IsOpen:           now >= r.OpensAt && now <= r.ClosesAt,
		Raw:              r,
		ViewerIsProvider: viewerIsProvider,
	}
	if len(r.CodeState) > 0 {
		s := base64.StdEncoding.EncodeToString(r.CodeState)
		out.CodeState = &s
	}
	return out
}

func toScores(scores []*marketplacev1.Score) []*model.FeedbackScore {
	out := make([]*model.FeedbackScore, len(scores))
	for i, sc := range scores {
		out[i] = &model.FeedbackScore{Criterion: up[string](sc.Criterion), Score: int(sc.Score)}
	}
	return out
}

func toNote(n *marketplacev1.MentorNote) *model.MentorNote {
	return &model.MentorNote{ID: n.Id, AtSeconds: int(n.AtSeconds), Body: n.Body, Tone: up[model.NoteTone](n.Tone), CreatedAt: unix(n.CreatedAt)}
}

func toQuestion(q *marketplacev1.Question) *model.Question {
	return &model.Question{
		ID: q.Id, Title: q.Title, Body: q.Body,
		InterviewType: up[model.InterviewType](q.InterviewType), Difficulty: up[model.Difficulty](q.Difficulty),
		Companies: nonNil(q.Companies), CreatedAt: unix(q.CreatedAt), Raw: q,
	}
}

func toExperience(e *communityv1.InterviewExperience) *model.InterviewExperience {
	rounds := make([]*model.ExperienceRound, len(e.Rounds))
	for i, r := range e.Rounds {
		rounds[i] = &model.ExperienceRound{Name: r.Name, Details: r.Details}
	}
	return &model.InterviewExperience{
		ID: e.Id, Company: e.Company, Role: e.Role, Year: int(e.Year),
		Outcome: up[model.ExperienceOutcome](e.Outcome), Rounds: rounds, Tips: e.Tips, Anonymous: e.Anonymous,
		Status: up[model.ExperienceStatus](e.Status), ReviewReason: e.ReviewReason, CreatedAt: unix(e.CreatedAt), Mine: e.Mine, Raw: e,
	}
}

func (r *Resolver) listExperiences(ctx context.Context, req *communityv1.ListExperiencesRequest) (*model.ExperiencePage, error) {
	a, err := r.actor(ctx)
	if err != nil {
		return nil, err
	}
	req.Actor = a
	res, err := r.CommunitySvc.ListExperiences(ctx, req)
	if err != nil {
		return nil, err
	}
	out := &model.ExperiencePage{Total: int(res.Total), Items: make([]*model.InterviewExperience, len(res.Experiences))}
	for i, e := range res.Experiences {
		out.Items[i] = toExperience(e)
	}
	return out, nil
}
