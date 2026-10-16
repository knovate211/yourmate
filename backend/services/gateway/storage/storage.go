// Package storage keeps uploaded files (avatars, post images, student ID
// documents) on local disk and serves them.
//
// Avatars and post images are public. Verification documents and resumes
// are private: they are only served through short-lived HMAC-signed URLs,
// handed to admins (IDs) or to the student and mentor (resumes).
// Swap this for S3/R2 by keeping the same Save/Delete/URL surface.
package storage

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Kinds of upload, which are also the key prefixes.
const (
	Avatars       = "avatars"
	Posts         = "posts"
	Verifications = "verifications"
	Resumes       = "resumes"
)

const MaxUploadBytes = 5 << 20

var (
	ErrTooLarge    = errors.New("file is larger than 5 MB")
	ErrUnsupported = errors.New("unsupported file type; upload a JPEG, PNG or WebP image")

	keyRe = regexp.MustCompile(`^(avatars|posts|verifications|resumes)/[0-9a-f-]{36}\.(jpg|png|webp|pdf)$`)

	imageTypes = map[string]string{"image/jpeg": "jpg", "image/png": "png", "image/webp": "webp"}
)

type Store struct {
	dir    string
	secret []byte
}

func New(dir, secret string) (*Store, error) {
	for _, k := range []string{Avatars, Posts, Verifications, Resumes} {
		if err := os.MkdirAll(filepath.Join(dir, k), 0o750); err != nil {
			return nil, err
		}
	}
	return &Store{dir: dir, secret: []byte(secret)}, nil
}

// Save validates the content by sniffing its bytes (never trusting the
// client's filename or content type) and stores it under a random key.
func (s *Store) Save(kind string, r io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxUploadBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > MaxUploadBytes {
		return "", ErrTooLarge
	}
	ctype := http.DetectContentType(data)
	if kind == Resumes {
		if ctype != "application/pdf" {
			return "", errors.New("unsupported file type; upload your resume as a PDF")
		}
		return s.write(kind, "pdf", data)
	}
	ext, ok := imageTypes[ctype]
	if !ok && kind == Verifications && ctype == "application/pdf" {
		ext, ok = "pdf", true
	}
	if !ok {
		if kind == Verifications {
			return "", errors.New("unsupported file type; upload a JPEG, PNG, WebP image or a PDF")
		}
		return "", ErrUnsupported
	}
	return s.write(kind, ext, data)
}

func (s *Store) write(kind, ext string, data []byte) (string, error) {
	key := fmt.Sprintf("%s/%s.%s", kind, uuid.NewString(), ext)
	if err := os.WriteFile(filepath.Join(s.dir, key), data, 0o640); err != nil {
		return "", err
	}
	return key, nil
}

func isPrivate(key string) bool {
	return strings.HasPrefix(key, Verifications+"/") || strings.HasPrefix(key, Resumes+"/")
}

// Delete removes a stored file; unknown keys are ignored.
func (s *Store) Delete(key string) {
	if keyRe.MatchString(key) {
		_ = os.Remove(filepath.Join(s.dir, key))
	}
}

// URL returns the public URL for a public key, or "" for none.
func (s *Store) URL(key string) string {
	if key == "" || isPrivate(key) {
		return ""
	}
	return "/files/" + key
}

// SignedURL returns a URL for a private key that works for ttl.
func (s *Store) SignedURL(key string, ttl time.Duration) string {
	if key == "" {
		return ""
	}
	if _, err := os.Stat(filepath.Join(s.dir, key)); err != nil {
		return ""
	}
	exp := strconv.FormatInt(time.Now().Add(ttl).Unix(), 10)
	return "/files/" + key + "?exp=" + exp + "&sig=" + s.sign(key, exp)
}

func (s *Store) sign(key, exp string) string {
	m := hmac.New(sha256.New, s.secret)
	m.Write([]byte(key + "|" + exp))
	return hex.EncodeToString(m.Sum(nil))
}

// ServeHTTP serves /files/<key>.
func (s *Store) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/files/")
	if !keyRe.MatchString(key) {
		http.NotFound(w, r)
		return
	}
	private := isPrivate(key)
	if private {
		exp, sig := r.URL.Query().Get("exp"), r.URL.Query().Get("sig")
		expUnix, err := strconv.ParseInt(exp, 10, 64)
		if err != nil || time.Now().Unix() > expUnix || !hmac.Equal([]byte(sig), []byte(s.sign(key, exp))) {
			http.Error(w, "link expired", http.StatusForbidden)
			return
		}
	}
	data, err := os.ReadFile(filepath.Join(s.dir, key))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", http.DetectContentType(data))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; style-src 'unsafe-inline'")
	if private {
		w.Header().Set("Cache-Control", "private, no-store")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
}
