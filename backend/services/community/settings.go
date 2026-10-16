package main

import (
	"context"
	"fmt"
	"time"

	communityv1 "github.com/yourmate/backend/gen/community/v1"
	"github.com/yourmate/backend/pkg/config"
)

// Community rules. These are the defaults; each one can be overridden with
// the environment variable named in loadSettings (see .env.example).
var (
	maxOwnedCommunities = 3                   // communities one student may own
	declineCooldown     = 7 * 24 * time.Hour  // wait after a declined join request
	removalBan          = 30 * 24 * time.Hour // ban after a moderator removes someone
	postsPerHour        = 20                  // community posts per person per hour
	repliesPerHour      = 60                  // community replies per person per hour
	maxPendingFeedPosts = 5                   // campus-feed posts waiting for review per person
	contactPerHour      = 3                   // contact-form messages per email per hour
	dmNewPerHour        = 10                  // new private chats one person may start per hour
	dmPendingLimit      = 3                   // messages allowed before a chat request is accepted
)

type option struct{ value, label string }

// The choices the service accepts. The gateway serves these lists (with
// labels) to the web app, so they are defined only here.
var (
	communityKindOptions = []option{
		{"general", "General"},
		{"course", "Course or batch"},
		{"hostel", "Hostel"},
		{"club", "Club"},
		{"interest", "Interest"},
		{"exam", "Exam prep"},
	}
	reportReasonOptions = []option{
		{"spam", "Spam or advertising"},
		{"harassment", "Harassment or bullying"},
		{"inappropriate", "Inappropriate content"},
		{"scam", "Scam or payment problem"},
		{"fake_profile", "Fake profile or not a student"},
		{"no_show", "Didn't show up"},
		{"other", "Something else"},
	}
	postTypeOptions = []option{
		{"event", "Event"},
		{"notice", "Notice"},
		{"place", "Place"},
		{"tip", "Tip"},
	}
	communityKinds = values(communityKindOptions)
	reportReasons  = values(reportReasonOptions)
	postTypes      = values(postTypeOptions)
	joinPolicies   = []string{"open", "approval"}
)

func values(opts []option) []string {
	out := make([]string, len(opts))
	for i, o := range opts {
		out[i] = o.value
	}
	return out
}

func init() { buildCommunityColumns() }

func loadSettings() {
	maxOwnedCommunities = config.Int("COMMUNITY_MAX_OWNED", maxOwnedCommunities)
	declineCooldown = config.Duration("COMMUNITY_DECLINE_COOLDOWN", declineCooldown)
	removalBan = config.Duration("COMMUNITY_REMOVAL_BAN", removalBan)
	postsPerHour = config.Int("COMMUNITY_POSTS_PER_HOUR", postsPerHour)
	repliesPerHour = config.Int("COMMUNITY_REPLIES_PER_HOUR", repliesPerHour)
	maxPendingFeedPosts = config.Int("FEED_MAX_PENDING_POSTS", maxPendingFeedPosts)
	contactPerHour = config.Int("CONTACT_PER_HOUR", contactPerHour)
	dmNewPerHour = config.Int("DM_NEW_PER_HOUR", dmNewPerHour)
	dmPendingLimit = config.Int("DM_PENDING_LIMIT", dmPendingLimit)
	buildCommunityColumns()
}

func humanDuration(d time.Duration) string {
	switch {
	case d%(24*time.Hour) == 0 && d >= 24*time.Hour:
		if n := int(d.Hours() / 24); n != 1 {
			return fmt.Sprintf("%d days", n)
		}
		return "1 day"
	case d%time.Hour == 0:
		return fmt.Sprintf("%d hours", int(d.Hours()))
	}
	return fmt.Sprintf("%d minutes", int(d.Minutes()))
}

func toOptions(opts []option) []*communityv1.Option {
	out := make([]*communityv1.Option, len(opts))
	for i, o := range opts {
		out[i] = &communityv1.Option{Value: o.value, Label: o.label}
	}
	return out
}

func (s *server) GetCatalog(context.Context, *communityv1.Empty) (*communityv1.CommunityCatalog, error) {
	return &communityv1.CommunityCatalog{
		CommunityKinds: toOptions(communityKindOptions),
		ReportReasons:  toOptions(reportReasonOptions),
		PostTypes:      toOptions(postTypeOptions),
	}, nil
}
