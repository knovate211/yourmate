package main

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	marketplacev1 "github.com/yourmate/backend/gen/marketplace/v1"
	"github.com/yourmate/backend/pkg/config"
)

// Marketplace rules. These are the defaults; each one can be overridden with
// the environment variable named in loadSettings (see .env.example).
var (
	leadTime         = 2 * time.Hour  // notice a provider gets before a session
	defaultWin       = 14             // days of open slots shown by default
	maxWin           = 28             // how far ahead sessions can be booked, in days
	maxOpenRequests  = 5              // pending requests one student may have at once
	autoCompleteWait = 48 * time.Hour // accepted sessions complete on their own after this
	sessionDurations = []int32{30, 60, 90, 120}
	allowedDurations = map[int32]bool{}

	resumeMaxOpen       = 3                   // resume reviews one student may have waiting at once
	resumeRequestTTL    = 72 * time.Hour      // unanswered resume requests expire after this
	resumeFileRetention = 30 * 24 * time.Hour // uploads are deleted this long after a review closes
	resumeMaxTurnaround = 14                  // longest turnaround a mentor may promise, in days

	roomOpensBefore = 10 * time.Minute // the video room opens this long before a session
	roomClosesAfter = 30 * time.Minute // and closes this long after it ends
)

type option struct{ value, label string }

// What sessions and guides can be; the gateway serves these lists (with
// labels) to the web app, so they are defined only here.
var (
	sessionModes        = []option{{"online", "Online"}, {"in_person", "In person"}}
	guideServiceOptions = []option{
		{"campus_tour", "Campus tour"},
		{"admissions", "Admissions help"},
		{"housing", "Hostel & housing"},
		{"city_orientation", "City orientation"},
		{"paperwork", "Paperwork & registration"},
		{"language_help", "Language help"},
		{"events", "Clubs & events"},
	}
	validModes    = values(sessionModes)
	guideServices = values(guideServiceOptions)

	// Career mentors.
	interviewTypeOptions = []option{
		{"coding", "Coding & DSA"},
		{"system_design", "System design"},
		{"frontend", "Frontend"},
		{"backend", "Backend"},
		{"data_ml", "Data science & ML"},
		{"product", "Product management"},
		{"case", "Consulting case"},
		{"hr", "HR & behavioural"},
	}
	// What a mock-interview scorecard rates, each 1-5.
	feedbackCriteriaOptions = []option{
		{"communication", "Communication"},
		{"problem_solving", "Problem solving"},
		{"technical_depth", "Technical depth"},
		{"structure", "Structured thinking"},
		{"confidence", "Confidence"},
	}
	verdictOptions = []option{
		{"ready", "Ready for the real thing"},
		{"almost", "Almost there"},
		{"not_yet", "Needs more practice"},
	}
	resumeSectionOptions = []option{
		{"summary", "Summary & headline"},
		{"education", "Education"},
		{"experience", "Experience & internships"},
		{"projects", "Projects"},
		{"skills", "Skills"},
		{"formatting", "Formatting & ATS"},
	}
	// Languages the interview room's code editor highlights.
	codeLanguageOptions = []option{
		{"python", "Python 3"},
		{"javascript", "JavaScript"},
		{"java", "Java"},
		{"cpp", "C++"},
		{"text", "Plain text"},
	}
	codeLanguages    = values(codeLanguageOptions)
	interviewTypes   = values(interviewTypeOptions)
	feedbackCriteria = values(feedbackCriteriaOptions)
	verdicts         = values(verdictOptions)
	resumeSections   = values(resumeSectionOptions)
)

func values(opts []option) []string {
	out := make([]string, len(opts))
	for i, o := range opts {
		out[i] = o.value
	}
	return out
}

func init() { setDurations(sessionDurations) }

func setDurations(ds []int32) {
	sessionDurations = ds
	allowedDurations = map[int32]bool{}
	for _, d := range ds {
		allowedDurations[d] = true
	}
}

func loadSettings() {
	leadTime = config.Duration("BOOKING_LEAD_TIME", leadTime)
	defaultWin = config.Int("BOOKING_DEFAULT_DAYS", defaultWin)
	maxWin = config.Int("BOOKING_MAX_DAYS", maxWin)
	maxOpenRequests = config.Int("BOOKING_MAX_OPEN_REQUESTS", maxOpenRequests)
	autoCompleteWait = config.Duration("BOOKING_AUTO_COMPLETE_AFTER", autoCompleteWait)
	if v := config.String("SESSION_DURATIONS", ""); v != "" {
		var ds []int32
		for _, p := range strings.Split(v, ",") {
			n, err := strconv.Atoi(strings.TrimSpace(p))
			if err != nil || n < 15 || n > 480 || n%15 != 0 {
				log.Fatalf("config: SESSION_DURATIONS must be minutes in steps of 15, got %q", p)
			}
			ds = append(ds, int32(n))
		}
		setDurations(ds)
	}
	resumeMaxOpen = config.Int("RESUME_MAX_OPEN", resumeMaxOpen)
	resumeRequestTTL = config.Duration("RESUME_REQUEST_TTL", resumeRequestTTL)
	resumeFileRetention = config.Duration("RESUME_FILE_RETENTION", resumeFileRetention)
	resumeMaxTurnaround = config.Int("RESUME_MAX_TURNAROUND_DAYS", resumeMaxTurnaround)
	roomOpensBefore = config.Duration("ROOM_OPENS_BEFORE", roomOpensBefore)
	roomClosesAfter = config.Duration("ROOM_CLOSES_AFTER", roomClosesAfter)
	if resumeMaxTurnaround < 1 || resumeMaxTurnaround > 30 {
		log.Fatalf("config: RESUME_MAX_TURNAROUND_DAYS must be 1-30")
	}
	if defaultWin > maxWin {
		defaultWin = maxWin
	}
}

// durationsText reads like "30, 60, 90 or 120 minutes".
func durationsText() string {
	parts := make([]string, len(sessionDurations))
	for i, d := range sessionDurations {
		parts[i] = strconv.Itoa(int(d))
	}
	if len(parts) == 1 {
		return parts[0] + " minutes"
	}
	return strings.Join(parts[:len(parts)-1], ", ") + " or " + parts[len(parts)-1] + " minutes"
}

func humanDuration(d time.Duration) string {
	if d%(24*time.Hour) == 0 {
		return fmt.Sprintf("%d days", int(d.Hours()/24))
	}
	if d%time.Hour == 0 {
		h := int(d.Hours())
		if h == 1 {
			return "1 hour"
		}
		return fmt.Sprintf("%d hours", h)
	}
	return fmt.Sprintf("%d minutes", int(d.Minutes()))
}

func toOptions(opts []option) []*marketplacev1.Option {
	out := make([]*marketplacev1.Option, len(opts))
	for i, o := range opts {
		out[i] = &marketplacev1.Option{Value: o.value, Label: o.label}
	}
	return out
}

func (s *server) GetCatalog(context.Context, *marketplacev1.Empty) (*marketplacev1.MarketplaceCatalog, error) {
	return &marketplacev1.MarketplaceCatalog{
		SessionDurations:  sessionDurations,
		GuideServices:     toOptions(guideServiceOptions),
		SessionModes:      toOptions(sessionModes),
		InterviewTypes:    toOptions(interviewTypeOptions),
		FeedbackCriteria:  toOptions(feedbackCriteriaOptions),
		ResumeSections:    toOptions(resumeSectionOptions),
		InterviewVerdicts: toOptions(verdictOptions),
		CodeLanguages:     toOptions(codeLanguageOptions),
	}, nil
}
