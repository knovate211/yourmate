package graph

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	marketplacev1 "github.com/yourmate/backend/gen/marketplace/v1"
)

// Live interview rooms run on LiveKit, an open-source video server. The
// gateway never carries video: it checks that the viewer is on the booking
// and inside the room's time window (the marketplace decides both), then
// signs a short-lived LiveKit pass for one room. LiveKit reports joins and
// leaves back through a signed webhook, which becomes the booking's join log.

// RoomConfig holds the LiveKit credentials. An empty Key turns rooms off.
type RoomConfig struct {
	URL    string // what browsers connect to, e.g. wss://rtc.yourmate.in
	Key    string
	Secret string
}

func roomName(bookingID string) string { return "booking-" + bookingID }

func bookingFromRoom(name string) (string, bool) {
	return strings.CutPrefix(name, "booking-")
}

type videoGrant struct {
	Room           string `json:"room"`
	RoomJoin       bool   `json:"roomJoin"`
	CanPublish     bool   `json:"canPublish"`
	CanSubscribe   bool   `json:"canSubscribe"`
	CanPublishData bool   `json:"canPublishData"`
}

type roomClaims struct {
	jwt.RegisteredClaims
	Name     string     `json:"name,omitempty"`
	Metadata string     `json:"metadata,omitempty"`
	Video    videoGrant `json:"video"`
}

// roomPass signs a LiveKit access token for one person in one room, valid
// until the room closes (and at least a few minutes).
func (c RoomConfig) roomPass(bookingID, userID, name, role string, closes time.Time) (string, time.Time, error) {
	now := time.Now()
	exp := closes
	if exp.Before(now.Add(5 * time.Minute)) {
		exp = now.Add(5 * time.Minute)
	}
	meta, _ := json.Marshal(map[string]string{"role": role})
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, roomClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    c.Key,
			Subject:   userID,
			NotBefore: jwt.NewNumericDate(now.Add(-time.Minute)),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
		Name:     name,
		Metadata: string(meta),
		Video:    videoGrant{Room: roomName(bookingID), RoomJoin: true, CanPublish: true, CanSubscribe: true, CanPublishData: true},
	})
	signed, err := tok.SignedString([]byte(c.Secret))
	return signed, exp, err
}

type webhookEvent struct {
	Event       string                `json:"event"`
	Room        struct{ Name string } `json:"room"`
	Participant struct {
		Identity string `json:"identity"`
		Name     string `json:"name"`
	} `json:"participant"`
	CreatedAt json.RawMessage `json:"createdAt"` // a number or, from protobuf JSON, a string
}

func (e webhookEvent) at() time.Time {
	s := strings.Trim(string(e.CreatedAt), `"`)
	if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 0 {
		return time.Unix(n, 0)
	}
	return time.Now()
}

// verifyWebhook checks LiveKit's signature: the Authorization header is a
// JWT signed with our API secret whose "sha256" claim is the body's hash.
func (c RoomConfig) verifyWebhook(auth string, body []byte) error {
	var claims struct {
		jwt.RegisteredClaims
		Sha256 string `json:"sha256"`
	}
	_, err := jwt.ParseWithClaims(strings.TrimPrefix(auth, "Bearer "), &claims, func(t *jwt.Token) (any, error) {
		return []byte(c.Secret), nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer(c.Key), jwt.WithLeeway(time.Minute))
	if err != nil {
		return err
	}
	sum := sha256.Sum256(body)
	want := base64.StdEncoding.EncodeToString(sum[:])
	if subtle.ConstantTimeCompare([]byte(claims.Sha256), []byte(want)) != 1 {
		return errors.New("body hash mismatch")
	}
	return nil
}

// RoomWebhook receives LiveKit's room events and records joins and leaves.
func RoomWebhook(r *Resolver) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost || r.Rooms.Key == "" {
			http.NotFound(w, req)
			return
		}
		body, err := io.ReadAll(io.LimitReader(req.Body, 1<<20))
		if err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		if err := r.Rooms.verifyWebhook(req.Header.Get("Authorization"), body); err != nil {
			slog.Warn("room webhook refused", "err", err)
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
		var ev webhookEvent
		if err := json.Unmarshal(body, &ev); err != nil {
			http.Error(w, "bad event", http.StatusBadRequest)
			return
		}
		bookingID, ok := bookingFromRoom(ev.Room.Name)
		var kind string
		switch ev.Event {
		case "participant_joined":
			kind = "joined"
		case "participant_left":
			kind = "left"
		}
		if ok && kind != "" && ev.Participant.Identity != "" {
			ctx := context.WithoutCancel(req.Context())
			if _, err := r.Market.RecordPresence(ctx, &marketplacev1.PresenceEvent{
				BookingId: bookingID, UserId: ev.Participant.Identity, Event: kind, At: ev.at().Unix()}); err != nil {
				slog.Warn("record room presence", "err", err)
			} else if kind == "joined" {
				r.nudgeOtherParty(ctx, bookingID, ev.Participant.Identity, ev.Participant.Name)
			}
		}
		w.WriteHeader(http.StatusOK)
	})
}

// nudgeOtherParty tells the other person someone is waiting, unless they're
// already in the room.
func (r *Resolver) nudgeOtherParty(ctx context.Context, bookingID, joinedID, name string) {
	room, err := r.Market.GetRoom(ctx, &marketplacev1.RoomRequest{BookingId: bookingID, ActorId: joinedID})
	if err != nil {
		return
	}
	b, err := r.Market.GetBooking(ctx, &marketplacev1.GetBookingRequest{Id: bookingID, ActorId: joinedID})
	if err != nil {
		return
	}
	other := b.ProviderId
	if joinedID == b.ProviderId {
		other = b.StudentId
	}
	joins := 0
	for _, p := range room.Presence {
		if p.UserId == other && p.LeftAt == 0 {
			return
		}
		if p.UserId == joinedID {
			joins++
		}
	}
	if joins > 1 {
		return // a reconnect, not news
	}
	if name == "" {
		name = "The other person"
	}
	r.notify(ctx, other, "room_waiting", name+" is waiting in the room", "Your session's video room is open. Join now.", "/interview/"+bookingID)
}
