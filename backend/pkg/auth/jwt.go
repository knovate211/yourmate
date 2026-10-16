// Package auth signs and verifies the short-lived access tokens that the
// identity service issues and the gateway checks on every request.
package auth

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const issuer = "yourmate"

// Claims is the access-token payload.
type Claims struct {
	Roles    []string `json:"roles"`
	CampusID string   `json:"cid"`
	jwt.RegisteredClaims
}

// IsAdmin reports whether the token carries the admin role.
func (c *Claims) IsAdmin() bool { return slices.Contains(c.Roles, "admin") }

// Signer issues and verifies HS256 access tokens.
type Signer struct {
	secret []byte
	ttl    time.Duration
}

// NewSigner returns a Signer; secret must be at least 32 bytes.
func NewSigner(secret string, ttl time.Duration) (*Signer, error) {
	if len(secret) < 32 {
		return nil, errors.New("auth: JWT secret must be at least 32 characters")
	}
	return &Signer{secret: []byte(secret), ttl: ttl}, nil
}

// Sign returns a token for the user and its expiry time.
func (s *Signer) Sign(userID, campusID string, roles []string) (string, time.Time, error) {
	now := time.Now()
	exp := now.Add(s.ttl)
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
		Roles:    roles,
		CampusID: campusID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			Issuer:    issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	})
	signed, err := tok.SignedString(s.secret)
	return signed, exp, err
}

// Verify parses and validates a token.
func (s *Signer) Verify(token string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method %v", t.Header["alg"])
		}
		return s.secret, nil
	}, jwt.WithIssuer(issuer), jwt.WithExpirationRequired())
	if err != nil {
		return nil, err
	}
	if claims.Subject == "" {
		return nil, errors.New("token has no subject")
	}
	return claims, nil
}
