package graph

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/99designs/gqlgen/graphql"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/vektah/gqlparser/v2/gqlerror"

	"github.com/yourmate/backend/pkg/auth"
)

type ctxKey int

const (
	viewerKey ctxKey = iota
	httpKey
	loadersKey
)

// Viewer is the authenticated caller, taken from the access token.
type Viewer struct {
	ID       string
	CampusID string
	Admin    bool
	Expires  time.Time // when the access token stops being valid
}

type httpCtx struct {
	w        http.ResponseWriter
	r        *http.Request
	clientIP string
}

// CookieConfig controls the refresh-token cookie.
type CookieConfig struct {
	Secure bool
	Domain string
}

const refreshCookie = "ym_refresh"

// Middleware authenticates the bearer token (if any) and attaches the
// request, the viewer and fresh per-request loaders to the context.
func Middleware(signer *auth.Signer, r *Resolver, trustProxy bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		if h := req.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			claims, err := signer.Verify(strings.TrimPrefix(h, "Bearer "))
			if err != nil {
				// An expired token is not an error for public queries, but the
				// client must know to refresh, so say so explicitly.
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"errors":[{"message":"session expired","extensions":{"code":"TOKEN_EXPIRED"}}]}`))
				return
			}
			ctx = context.WithValue(ctx, viewerKey, viewerFrom(claims))
		}
		ctx = context.WithValue(ctx, httpKey, &httpCtx{w: w, r: req, clientIP: clientIP(req, trustProxy)})
		ctx = context.WithValue(ctx, loadersKey, newLoaders(r))
		next.ServeHTTP(w, req.WithContext(ctx))
	})
}

func viewerFrom(c *auth.Claims) *Viewer {
	return &Viewer{ID: c.Subject, CampusID: c.CampusID, Admin: c.IsAdmin(), Expires: c.ExpiresAt.Time}
}

// WebsocketInit authenticates a WebSocket connection. Browsers can't set
// headers on a WebSocket, so the access token arrives in the graphql-ws
// connection_init payload as {"Authorization": "Bearer …"}.
func WebsocketInit(signer *auth.Signer) func(context.Context, transport.InitPayload) (context.Context, *transport.InitPayload, error) {
	return func(ctx context.Context, p transport.InitPayload) (context.Context, *transport.InitPayload, error) {
		token := strings.TrimPrefix(p.Authorization(), "Bearer ")
		if token == "" {
			return ctx, nil, nil // anonymous: @auth subscriptions will refuse
		}
		claims, err := signer.Verify(token)
		if err != nil {
			return nil, nil, errors.New("session expired")
		}
		return context.WithValue(ctx, viewerKey, viewerFrom(claims)), nil, nil
	}
}

func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			return strings.TrimSpace(strings.Split(xff, ",")[0])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func viewer(ctx context.Context) *Viewer {
	v, _ := ctx.Value(viewerKey).(*Viewer)
	return v
}

func viewerID(ctx context.Context) string {
	if v := viewer(ctx); v != nil {
		return v.ID
	}
	return ""
}

func isAdmin(ctx context.Context) bool {
	v := viewer(ctx)
	return v != nil && v.Admin
}

func httpFrom(ctx context.Context) *httpCtx {
	h, _ := ctx.Value(httpKey).(*httpCtx)
	return h
}

func userAgent(ctx context.Context) string {
	if h := httpFrom(ctx); h != nil {
		return h.r.UserAgent()
	}
	return ""
}

func (r *Resolver) setRefreshCookie(ctx context.Context, token string, expires int64) {
	h := httpFrom(ctx)
	if h == nil {
		return
	}
	http.SetCookie(h.w, &http.Cookie{
		Name:     refreshCookie,
		Value:    token,
		Path:     "/graphql",
		Domain:   r.Cookies.Domain,
		Expires:  time.Unix(expires, 0),
		HttpOnly: true,
		Secure:   r.Cookies.Secure,
		// Strict: the cookie never rides along on cross-site requests, which
		// is what keeps the refresh mutation safe from CSRF.
		SameSite: http.SameSiteStrictMode,
	})
}

func (r *Resolver) clearRefreshCookie(ctx context.Context) {
	if h := httpFrom(ctx); h != nil {
		http.SetCookie(h.w, &http.Cookie{Name: refreshCookie, Value: "", Path: "/graphql", Domain: r.Cookies.Domain,
			MaxAge: -1, HttpOnly: true, Secure: r.Cookies.Secure, SameSite: http.SameSiteStrictMode})
	}
}

func readRefreshCookie(ctx context.Context) string {
	h := httpFrom(ctx)
	if h == nil {
		return ""
	}
	c, err := h.r.Cookie(refreshCookie)
	if err != nil {
		return ""
	}
	return c.Value
}

// AuthDirective implements @auth.
func AuthDirective(ctx context.Context, _ any, next graphql.Resolver) (any, error) {
	if viewer(ctx) == nil {
		return nil, &gqlerror.Error{Message: "log in to continue", Extensions: map[string]any{"code": "UNAUTHENTICATED"}}
	}
	return next(ctx)
}

// AdminDirective implements @admin.
func AdminDirective(ctx context.Context, _ any, next graphql.Resolver) (any, error) {
	v := viewer(ctx)
	if v == nil {
		return nil, &gqlerror.Error{Message: "log in to continue", Extensions: map[string]any{"code": "UNAUTHENTICATED"}}
	}
	if !v.Admin {
		return nil, &gqlerror.Error{Message: "admins only", Extensions: map[string]any{"code": "FORBIDDEN"}}
	}
	return next(ctx)
}
