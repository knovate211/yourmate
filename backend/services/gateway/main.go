// Command gateway serves the public GraphQL API at /graphql and uploaded
// files at /files/, translating every request into gRPC calls to the
// identity, marketplace and community services.
package main

import (
	"bufio"
	"context"
	"errors"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/lru"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/99designs/gqlgen/graphql/playground"
	coderws "github.com/coder/websocket"
	"github.com/vektah/gqlparser/v2/ast"
	"google.golang.org/grpc"

	communityv1 "github.com/yourmate/backend/gen/community/v1"
	identityv1 "github.com/yourmate/backend/gen/identity/v1"
	marketplacev1 "github.com/yourmate/backend/gen/marketplace/v1"
	"github.com/yourmate/backend/pkg/auth"
	"github.com/yourmate/backend/pkg/config"
	"github.com/yourmate/backend/pkg/grpcx"
	"github.com/yourmate/backend/services/gateway/graph"
	"github.com/yourmate/backend/services/gateway/storage"
)

func main() {
	grpcx.SetupLogging("gateway", config.Bool("DEBUG", false))
	dev := config.Bool("DEV_MODE", false)
	token := config.MustString("INTERNAL_TOKEN")

	dial := func(addr string) *grpc.ClientConn {
		conn, err := grpcx.Dial(addr, token)
		if err != nil {
			log.Fatal(err)
		}
		return conn
	}
	idConn := dial(config.String("IDENTITY_ADDR", "localhost:50071"))
	mkConn := dial(config.String("MARKETPLACE_ADDR", "localhost:50072"))
	cmConn := dial(config.String("COMMUNITY_ADDR", "localhost:50073"))

	signer, err := auth.NewSigner(config.MustString("JWT_SECRET"), time.Minute)
	if err != nil {
		log.Fatal(err)
	}
	store, err := storage.New(config.String("UPLOAD_DIR", "./uploads"), config.MustString("FILE_SIGNING_SECRET"))
	if err != nil {
		log.Fatal(err)
	}

	res := &graph.Resolver{
		Identity:     identityv1.NewIdentityServiceClient(idConn),
		Market:       marketplacev1.NewMarketplaceServiceClient(mkConn),
		CommunitySvc: communityv1.NewCommunityServiceClient(cmConn),
		Store:        store,
		Cookies:      graph.CookieConfig{Secure: config.Bool("COOKIE_SECURE", !dev), Domain: config.String("COOKIE_DOMAIN", "")},
		Limiter:      graph.NewLimiter(),
		// Video rooms: off unless LiveKit credentials are configured.
		Rooms: graph.RoomConfig{
			URL:    config.String("LIVEKIT_PUBLIC_URL", ""),
			Key:    config.String("LIVEKIT_API_KEY", ""),
			Secret: config.String("LIVEKIT_API_SECRET", ""),
		},
	}

	go purgeResumeFiles(context.Background(), res.Market, store, config.Duration("RESUME_PURGE_INTERVAL", time.Hour))

	gql := handler.New(graph.NewExecutableSchema(graph.Config{
		Resolvers:  res,
		Directives: graph.DirectiveRoot{Auth: graph.AuthDirective, Admin: graph.AdminDirective},
	}))
	origins := strings.Split(config.String("CORS_ORIGINS", "http://localhost:5190"), ",")
	// WebSocket first: it claims only upgrade requests. Origins are checked
	// against the same list as CORS, so other sites can't open a socket with
	// a visitor's browser.
	gql.AddTransport(transport.Websocket{
		Implementation:        transport.CoderWebsocketImplementation{AcceptOptions: coderws.AcceptOptions{OriginPatterns: originHosts(origins)}},
		InitFunc:              graph.WebsocketInit(signer),
		KeepAlivePingInterval: 20 * time.Second,
	})
	gql.AddTransport(transport.Options{})
	gql.AddTransport(transport.POST{})
	gql.AddTransport(transport.MultipartForm{MaxUploadSize: storage.MaxUploadBytes + 1<<20, MaxMemory: 8 << 20})
	gql.SetQueryCache(lru.New[*ast.QueryDocument](1000))
	gql.Use(extension.FixedComplexityLimit(5000))
	if dev {
		gql.Use(extension.Introspection{})
	}
	gql.Use(extension.AutomaticPersistedQuery{Cache: lru.New[string](100)})
	gql.SetErrorPresenter(graph.PresentError)
	gql.SetRecoverFunc(graph.RecoverPanic)

	mux := http.NewServeMux()
	mux.Handle("/graphql", graph.Middleware(signer, res, config.Bool("TRUST_PROXY", false), gql))
	mux.Handle("/files/", store)
	mux.Handle("/livekit/webhook", graph.RoomWebhook(res))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	if dev {
		mux.Handle("/playground", playground.Handler("Your Mate API", "/graphql"))
	}

	srv := &http.Server{
		Addr:              config.String("HTTP_ADDR", ":8095"),
		Handler:           withSocketDeadlines(withCORS(origins, withSecurityHeaders(withLogging(mux)))),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()
	slog.Info("gateway listening", "addr", srv.Addr, "dev", dev)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	idConn.Close()
	mkConn.Close()
	cmConn.Close()
}

// purgeResumeFiles deletes the uploads of resume reviews that closed long
// ago. The marketplace decides which ones (and forgets their keys); the
// gateway owns the files.
func purgeResumeFiles(ctx context.Context, market marketplacev1.MarketplaceServiceClient, store *storage.Store, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		for {
			res, err := market.ClaimExpiredResumeFiles(ctx, &marketplacev1.Empty{})
			if err != nil {
				slog.Warn("resume file purge", "err", err)
				break
			}
			for _, k := range res.Keys {
				store.Delete(k)
			}
			if len(res.Keys) > 0 {
				slog.Info("purged resume files", "files", len(res.Keys))
			}
			if len(res.Keys) < 500 { // a full batch means there may be more
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// originHosts turns "http://localhost:5190" into "localhost:5190", the form
// the WebSocket origin check expects.
func originHosts(origins []string) []string {
	out := make([]string, 0, len(origins))
	for _, o := range origins {
		if u, err := url.Parse(strings.TrimSpace(o)); err == nil && u.Host != "" {
			out = append(out, u.Host)
		}
	}
	return out
}

// withSocketDeadlines lifts the server's read/write timeouts for WebSocket
// upgrades: a live connection stays open far longer than any request. The
// keep-alive pings detect dead peers instead.
func withSocketDeadlines(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			rc := http.NewResponseController(w)
			_ = rc.SetReadDeadline(time.Time{})
			_ = rc.SetWriteDeadline(time.Time{})
		}
		next.ServeHTTP(w, r)
	})
}

func withCORS(origins []string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if o := r.Header.Get("Origin"); o != "" && slices.Contains(origins, o) {
			w.Header().Set("Access-Control-Allow-Origin", o)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Apollo-Require-Preflight")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Add("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		// Multipart uploads are "simple" requests that skip CORS preflight, so
		// require a custom header on them; browsers can't add one cross-site.
		if r.Method == http.MethodPost && strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") &&
			r.Header.Get("Apollo-Require-Preflight") == "" {
			http.Error(w, "missing Apollo-Require-Preflight header", http.StatusBadRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Hijack lets WebSocket upgrades through the logging wrapper.
func (s *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := s.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("connection can't be hijacked")
	}
	s.status = http.StatusSwitchingProtocols
	return h.Hijack()
}

func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rec, r)
		if r.URL.Path != "/healthz" {
			slog.Debug("http", "method", r.Method, "path", r.URL.Path, "status", rec.status, "ms", time.Since(start).Milliseconds())
		}
	})
}
