// Package grpcx holds the gRPC plumbing shared by every service: the
// internal-token guard, panic recovery, request logging and client dialing.
package grpcx

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// TokenHeader carries the shared secret that proves a call came from inside
// the platform. Services are never exposed publicly, but the check keeps a
// misconfigured port from becoming an unauthenticated admin API.
const TokenHeader = "x-internal-token"

// NewServer returns a server that rejects calls without the internal token,
// recovers panics and logs every call.
func NewServer(internalToken string) *grpc.Server {
	srv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			recoverInterceptor,
			logInterceptor,
			tokenInterceptor(internalToken),
		),
		// Streams (live events) need the same guard as single calls.
		grpc.ChainStreamInterceptor(
			recoverStreamInterceptor,
			tokenStreamInterceptor(internalToken),
		),
	)
	healthpb.RegisterHealthServer(srv, health.NewServer())
	return srv
}

// Serve listens on addr and stops gracefully on SIGINT/SIGTERM.
func Serve(srv *grpc.Server, addr string) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		slog.Info("shutting down")
		srv.GracefulStop()
	}()
	slog.Info("grpc listening", "addr", addr)
	return srv.Serve(lis)
}

// Dial opens a client connection that attaches the internal token to every
// call and stream.
func Dial(addr, internalToken string) (*grpc.ClientConn, error) {
	return grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
			ctx = metadata.AppendToOutgoingContext(ctx, TokenHeader, internalToken)
			return invoker(ctx, method, req, reply, cc, opts...)
		}),
		grpc.WithStreamInterceptor(func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
			ctx = metadata.AppendToOutgoingContext(ctx, TokenHeader, internalToken)
			return streamer(ctx, desc, cc, method, opts...)
		}),
	)
}

func validToken(ctx context.Context, token string) bool {
	md, _ := metadata.FromIncomingContext(ctx)
	got := md.Get(TokenHeader)
	return len(got) == 1 && subtle.ConstantTimeCompare([]byte(got[0]), []byte(token)) == 1
}

func tokenStreamInterceptor(token string) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if !validToken(ss.Context(), token) {
			return status.Error(codes.Unauthenticated, "missing or invalid internal token")
		}
		return handler(srv, ss)
	}
}

func recoverStreamInterceptor(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic in stream", "method", info.FullMethod, "panic", r, "stack", string(debug.Stack()))
			err = status.Error(codes.Internal, "internal error")
		}
	}()
	return handler(srv, ss)
}

func tokenInterceptor(token string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if info.FullMethod == "/grpc.health.v1.Health/Check" {
			return handler(ctx, req)
		}
		if !validToken(ctx, token) {
			return nil, status.Error(codes.Unauthenticated, "missing or invalid internal token")
		}
		return handler(ctx, req)
	}
}

func recoverInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic in handler", "method", info.FullMethod, "panic", r, "stack", string(debug.Stack()))
			err = status.Error(codes.Internal, "internal error")
		}
	}()
	return handler(ctx, req)
}

func logInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	start := time.Now()
	resp, err := handler(ctx, req)
	code := status.Code(err)
	attrs := []any{"method", info.FullMethod, "code", code.String(), "ms", time.Since(start).Milliseconds()}
	switch code {
	case codes.OK, codes.NotFound, codes.InvalidArgument, codes.AlreadyExists, codes.FailedPrecondition, codes.PermissionDenied, codes.Unauthenticated, codes.ResourceExhausted:
		slog.Debug("rpc", attrs...)
	default:
		slog.Error("rpc", append(attrs, "err", err)...)
	}
	return resp, err
}

// Errorf helpers keep handler code short.

func Invalid(format string, a ...any) error {
	return status.Errorf(codes.InvalidArgument, format, a...)
}
func NotFound(format string, a ...any) error { return status.Errorf(codes.NotFound, format, a...) }
func Denied(format string, a ...any) error {
	return status.Errorf(codes.PermissionDenied, format, a...)
}
func Precondition(format string, a ...any) error {
	return status.Errorf(codes.FailedPrecondition, format, a...)
}
func Exists(format string, a ...any) error {
	return status.Errorf(codes.AlreadyExists, format, a...)
}
func Unauthenticated(format string, a ...any) error {
	return status.Errorf(codes.Unauthenticated, format, a...)
}
func TooMany(format string, a ...any) error {
	return status.Errorf(codes.ResourceExhausted, format, a...)
}

// Internal logs the real cause and returns an opaque error to the caller.
func Internal(err error) error {
	slog.Error("internal error", "err", err)
	return status.Error(codes.Internal, "internal error")
}

// SetupLogging configures slog for a service.
func SetupLogging(service string, debugLevel bool) {
	level := slog.LevelInfo
	if debugLevel {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})).With("service", service))
}
