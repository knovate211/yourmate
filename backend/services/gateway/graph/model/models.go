// Package model holds the GraphQL types. Types with relations or
// viewer-dependent fields are declared here and keep the source proto in Raw;
// gqlgen generates resolvers for every schema field missing from the struct.
package model

import (
	"time"

	communityv1 "github.com/yourmate/backend/gen/community/v1"
	identityv1 "github.com/yourmate/backend/gen/identity/v1"
	marketplacev1 "github.com/yourmate/backend/gen/marketplace/v1"
)

type User struct {
	ID             string    `json:"id"`
	FirstName      string    `json:"firstName"`
	Bio            string    `json:"bio"`
	CampusVerified bool      `json:"campusVerified"`
	IDVerified     bool      `json:"idVerified"`
	JoinedAt       time.Time `json:"joinedAt"`
	IsAdmin        bool      `json:"isAdmin"`

	Raw *identityv1.User `json:"-"`
}

type Provider struct {
	ID                string         `json:"id"`
	Kind              ProviderKind   `json:"kind"`
	DisplayName       string         `json:"displayName"`
	Headline          string         `json:"headline"`
	Bio               string         `json:"bio"`
	Rate              *Money         `json:"rate"`
	Modes             []SessionMode  `json:"modes"`
	Languages         []string       `json:"languages"`
	Services          []GuideService `json:"services"`
	Status            ProviderStatus `json:"status"`
	RatingAvg         float64        `json:"ratingAvg"`
	RatingCount       int            `json:"ratingCount"`
	SessionsCompleted int            `json:"sessionsCompleted"`
	CreatedAt         time.Time      `json:"createdAt"`

	Raw *marketplacev1.ProviderProfile `json:"-"`
}

type Booking struct {
	ID              string        `json:"id"`
	Kind            ProviderKind  `json:"kind"`
	Service         *GuideService `json:"service"`
	StartAt         time.Time     `json:"startAt"`
	EndAt           time.Time     `json:"endAt"`
	DurationMinutes int           `json:"durationMinutes"`
	Mode            SessionMode   `json:"mode"`
	Location        string        `json:"location"`
	Price           *Money        `json:"price"`
	Notes           string        `json:"notes"`
	Status          BookingStatus `json:"status"`
	StatusReason    string        `json:"statusReason"`
	Reviewed        bool          `json:"reviewed"`
	CreatedAt       time.Time     `json:"createdAt"`
	UpdatedAt       time.Time     `json:"updatedAt"`

	Raw *marketplacev1.Booking `json:"-"`
}

type Message struct {
	ID        string    `json:"id"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`

	Raw *marketplacev1.Message `json:"-"`
}

type Review struct {
	ID        string    `json:"id"`
	Rating    int       `json:"rating"`
	Comment   string    `json:"comment"`
	Hidden    bool      `json:"hidden"`
	CreatedAt time.Time `json:"createdAt"`

	Raw *marketplacev1.Review `json:"-"`
}

type Verification struct {
	ID         *string            `json:"id"`
	Status     VerificationStatus `json:"status"`
	Reason     string             `json:"reason"`
	CreatedAt  *time.Time         `json:"createdAt"`
	ReviewedAt *time.Time         `json:"reviewedAt"`

	Raw *identityv1.Verification `json:"-"`
}

type Post struct {
	ID           string     `json:"id"`
	Type         PostType   `json:"type"`
	Title        string     `json:"title"`
	Body         string     `json:"body"`
	EventAt      *time.Time `json:"eventAt"`
	Location     string     `json:"location"`
	Status       PostStatus `json:"status"`
	ReviewReason string     `json:"reviewReason"`
	CreatedAt    time.Time  `json:"createdAt"`

	Raw *communityv1.Post `json:"-"`
}

type Report struct {
	ID             string       `json:"id"`
	TargetType     ReportTarget `json:"targetType"`
	TargetID       string       `json:"targetId"`
	Reason         ReportReason `json:"reason"`
	Details        string       `json:"details"`
	Status         ReportStatus `json:"status"`
	ResolutionNote string       `json:"resolutionNote"`
	CreatedAt      time.Time    `json:"createdAt"`
	ResolvedAt     *time.Time   `json:"resolvedAt"`

	Raw *communityv1.Report `json:"-"`
}

type Community struct {
	ID          string        `json:"id"`
	Slug        string        `json:"slug"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Kind        CommunityKind `json:"kind"`
	JoinPolicy  JoinPolicy    `json:"joinPolicy"`
	Rules       string        `json:"rules"`
	IsDefault   bool          `json:"isDefault"`
	MemberCount int           `json:"memberCount"`
	PostCount   int           `json:"postCount"`
	CreatedAt   time.Time     `json:"createdAt"`

	Raw *communityv1.Community `json:"-"`
}

type CommunityPost struct {
	ID         string    `json:"id"`
	Body       string    `json:"body"`
	Pinned     bool      `json:"pinned"`
	LikeCount  int       `json:"likeCount"`
	ReplyCount int       `json:"replyCount"`
	LikedByMe  bool      `json:"likedByMe"`
	CreatedAt  time.Time `json:"createdAt"`

	Raw *communityv1.CommunityPost `json:"-"`
}

type CommunityReply struct {
	ID        string    `json:"id"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`

	Raw *communityv1.Reply `json:"-"`
}

type CommunityMember struct {
	Role     CommunityRole `json:"role"`
	JoinedAt time.Time     `json:"joinedAt"`

	Raw *communityv1.Member `json:"-"`
}

type JoinRequest struct {
	ID        string    `json:"id"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"createdAt"`

	Raw *communityv1.JoinRequest `json:"-"`
}

type Conversation struct {
	ID              string             `json:"id"`
	Status          ConversationStatus `json:"status"`
	CanSend         bool               `json:"canSend"`
	SharesCommunity bool               `json:"sharesCommunity"`
	UnreadCount     int                `json:"unreadCount"`
	UpdatedAt       time.Time          `json:"updatedAt"`

	Raw *communityv1.Conversation `json:"-"`
}

type DirectMessage struct {
	ID        string    `json:"id"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`

	Raw *communityv1.DirectMessage `json:"-"`
}

type ResumeReview struct {
	ID             string               `json:"id"`
	TargetRole     string               `json:"targetRole"`
	Notes          string               `json:"notes"`
	FileName       string               `json:"fileName"`
	Price          *Money               `json:"price"`
	TurnaroundDays int                  `json:"turnaroundDays"`
	Status         ResumeReviewStatus   `json:"status"`
	StatusReason   string               `json:"statusReason"`
	DueAt          *time.Time           `json:"dueAt"`
	AcceptedAt     *time.Time           `json:"acceptedAt"`
	DeliveredAt    *time.Time           `json:"deliveredAt"`
	OverallScore   *int                 `json:"overallScore"`
	Summary        string               `json:"summary"`
	Sections       []*ResumeSectionNote `json:"sections"`
	Rated          bool                 `json:"rated"`
	CreatedAt      time.Time            `json:"createdAt"`
	UpdatedAt      time.Time            `json:"updatedAt"`

	Raw *marketplacev1.ResumeReview `json:"-"`
}

type InterviewRoom struct {
	Problem     string           `json:"problem"`
	Language    string           `json:"language"`
	Code        string           `json:"code"`
	CodeState   *string          `json:"codeState"`
	Whiteboard  string           `json:"whiteboard"`
	DraftScores []*FeedbackScore `json:"draftScores"`
	OpensAt     time.Time        `json:"opensAt"`
	ClosesAt    time.Time        `json:"closesAt"`
	IsOpen      bool             `json:"isOpen"`

	Raw              *marketplacev1.Room `json:"-"`
	ViewerIsProvider bool                `json:"-"`
}

type RoomPresence struct {
	JoinedAt time.Time  `json:"joinedAt"`
	LeftAt   *time.Time `json:"leftAt"`

	Raw *marketplacev1.Presence `json:"-"`
}

type Question struct {
	ID            string        `json:"id"`
	Title         string        `json:"title"`
	Body          string        `json:"body"`
	InterviewType InterviewType `json:"interviewType"`
	Difficulty    Difficulty    `json:"difficulty"`
	Companies     []string      `json:"companies"`
	CreatedAt     time.Time     `json:"createdAt"`

	Raw *marketplacev1.Question `json:"-"`
}

type ProviderSlot struct {
	Provider *Provider `json:"provider"`
	StartAt  time.Time `json:"startAt"`
}

type ProgressInterview struct {
	InterviewType *InterviewType   `json:"interviewType"`
	At            time.Time        `json:"at"`
	Verdict       InterviewVerdict `json:"verdict"`
	Scores        []*FeedbackScore `json:"scores"`

	Raw *marketplacev1.ProgressInterview `json:"-"`
}

type InterviewExperience struct {
	ID           string             `json:"id"`
	Company      string             `json:"company"`
	Role         string             `json:"role"`
	Year         int                `json:"year"`
	Outcome      ExperienceOutcome  `json:"outcome"`
	Rounds       []*ExperienceRound `json:"rounds"`
	Tips         string             `json:"tips"`
	Anonymous    bool               `json:"anonymous"`
	Status       ExperienceStatus   `json:"status"`
	ReviewReason string             `json:"reviewReason"`
	CreatedAt    time.Time          `json:"createdAt"`
	Mine         bool               `json:"mine"`

	Raw *communityv1.InterviewExperience `json:"-"`
}

type MessageEvent struct {
	ConversationID string         `json:"conversationId"`
	Message        *DirectMessage `json:"message"`
}
