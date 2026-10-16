package graph

import (
	"context"
	"sync"
	"time"
)

// Limiter is a fixed-window counter keyed by action and client IP. It only
// guards the unauthenticated, abuse-prone mutations (signup, login, OTP,
// contact); per-account limits live in the services. A single gateway
// instance is assumed; with several, move this to Redis.
type Limiter struct {
	mu      sync.Mutex
	windows map[string]*fixedWindow
}

type fixedWindow struct {
	start time.Time
	count int
}

func NewLimiter() *Limiter {
	l := &Limiter{windows: map[string]*fixedWindow{}}
	go l.sweep()
	return l
}

func (l *Limiter) allow(key string, limit int, per time.Duration) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	w, ok := l.windows[key]
	if !ok || now.Sub(w.start) >= per {
		l.windows[key] = &fixedWindow{start: now, count: 1}
		return true
	}
	if w.count >= limit {
		return false
	}
	w.count++
	return true
}

func (l *Limiter) sweep() {
	for range time.Tick(5 * time.Minute) {
		l.mu.Lock()
		for k, w := range l.windows {
			if time.Since(w.start) > time.Hour {
				delete(l.windows, k)
			}
		}
		l.mu.Unlock()
	}
}

// limit allows n calls of action per window from the caller's IP.
func (r *Resolver) limit(ctx context.Context, action string, n int, per time.Duration) error {
	ip := "unknown"
	if h := httpFrom(ctx); h != nil {
		ip = h.clientIP
	}
	if !r.Limiter.allow(action+"|"+ip, n, per) {
		return rateLimited()
	}
	return nil
}
