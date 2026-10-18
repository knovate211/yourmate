package graph

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var testRooms = RoomConfig{URL: "ws://localhost:7880", Key: "APIkey", Secret: "a-long-enough-test-secret-0123456789"}

func TestRoomPass(t *testing.T) {
	closes := time.Now().Add(time.Hour)
	tok, exp, err := testRooms.roomPass("b1", "u1", "Isha K.", "student", closes)
	if err != nil {
		t.Fatal(err)
	}
	if exp.Unix() != closes.Unix() {
		t.Fatalf("pass should expire when the room closes: %v vs %v", exp, closes)
	}
	var c roomClaims
	if _, err := jwt.ParseWithClaims(tok, &c, func(*jwt.Token) (any, error) { return []byte(testRooms.Secret), nil }); err != nil {
		t.Fatal(err)
	}
	if c.Issuer != "APIkey" || c.Subject != "u1" || c.Video.Room != "booking-b1" || !c.Video.RoomJoin || !c.Video.CanPublishData {
		t.Fatalf("claims: %+v", c)
	}
	// A room that already closed still gets a few minutes, never an expired pass.
	if _, exp, _ := testRooms.roomPass("b1", "u1", "", "student", time.Now().Add(-time.Hour)); time.Until(exp) < 4*time.Minute {
		t.Fatalf("expired pass: %v", exp)
	}
}

func webhookAuth(t *testing.T, secret string, body []byte) string {
	sum := sha256.Sum256(body)
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss": testRooms.Key, "exp": time.Now().Add(time.Minute).Unix(), "sha256": base64.StdEncoding.EncodeToString(sum[:]),
	})
	s, err := tok.SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestVerifyWebhook(t *testing.T) {
	body := []byte(`{"event":"participant_joined","room":{"name":"booking-b1"},"participant":{"identity":"u1"}}`)
	if err := testRooms.verifyWebhook(webhookAuth(t, testRooms.Secret, body), body); err != nil {
		t.Fatalf("valid webhook refused: %v", err)
	}
	if err := testRooms.verifyWebhook(webhookAuth(t, testRooms.Secret, body), append(body, ' ')); err == nil {
		t.Fatal("tampered body accepted")
	}
	if err := testRooms.verifyWebhook(webhookAuth(t, "some-other-secret-0123456789abcdef", body), body); err == nil {
		t.Fatal("wrong secret accepted")
	}
	if err := testRooms.verifyWebhook("", body); err == nil {
		t.Fatal("missing signature accepted")
	}
}

func TestWebhookEventTime(t *testing.T) {
	for _, raw := range []string{`1700000000`, `"1700000000"`} {
		if got := (webhookEvent{CreatedAt: []byte(raw)}).at().Unix(); got != 1700000000 {
			t.Fatalf("%s → %d", raw, got)
		}
	}
}
