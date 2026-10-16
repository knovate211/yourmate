package graph

import (
	"context"
	"errors"
	"log/slog"
	"regexp"

	"github.com/99designs/gqlgen/graphql"
	"github.com/vektah/gqlparser/v2/gqlerror"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var grpcToGQL = map[codes.Code]string{
	codes.InvalidArgument:    "BAD_USER_INPUT",
	codes.NotFound:           "NOT_FOUND",
	codes.AlreadyExists:      "CONFLICT",
	codes.PermissionDenied:   "FORBIDDEN",
	codes.Unauthenticated:    "UNAUTHENTICATED",
	codes.FailedPrecondition: "FAILED_PRECONDITION",
	codes.ResourceExhausted:  "RATE_LIMITED",
}

// Services may prefix a message with a machine-readable reason, e.g.
// "EMAIL_NOT_VERIFIED: verify your email". The prefix becomes
// extensions.reason so the client can branch on it.
var reasonRe = regexp.MustCompile(`^([A-Z][A-Z_]+): (.+)$`)

// PresentError turns gRPC status errors into GraphQL errors with a stable
// extensions.code; anything unexpected is logged and hidden.
func PresentError(ctx context.Context, err error) *gqlerror.Error {
	gerr := graphql.DefaultErrorPresenter(ctx, err)
	var ue *userError
	if errors.As(err, &ue) {
		gerr.Message = ue.msg
		gerr.Extensions = map[string]any{"code": ue.code}
		return gerr
	}
	// Errors can reach the presenter more than once (directives wrap field
	// resolvers), so look through the wrapping for the original gRPC status.
	var se interface{ GRPCStatus() *status.Status }
	if errors.As(err, &se) {
		st := se.GRPCStatus()
		if code, known := grpcToGQL[st.Code()]; known {
			msg := st.Message()
			ext := map[string]any{"code": code}
			if m := reasonRe.FindStringSubmatch(msg); m != nil {
				ext["reason"], msg = m[1], m[2]
			}
			gerr.Message = msg
			gerr.Extensions = ext
			return gerr
		}
	} else if gerr.Extensions["code"] != nil {
		return gerr // directive errors and gqlgen's own validation errors
	}
	slog.Error("resolver error", "path", gerr.Path, "err", err)
	gerr.Message = "something went wrong; please try again"
	gerr.Extensions = map[string]any{"code": "INTERNAL"}
	return gerr
}

// RecoverPanic logs a panic in a resolver and returns an opaque error.
func RecoverPanic(_ context.Context, p any) error {
	slog.Error("resolver panic", "panic", p)
	return errors.New("internal error")
}

type userError struct{ code, msg string }

func (e *userError) Error() string { return e.msg }

func badInput(msg string) error  { return &userError{"BAD_USER_INPUT", msg} }
func forbidden(msg string) error { return &userError{"FORBIDDEN", msg} }
func rateLimited() error {
	return &userError{"RATE_LIMITED", "too many attempts from your network; wait a few minutes and try again"}
}

func isNotFound(err error) bool { return status.Code(err) == codes.NotFound }

func isUnauthenticated(err error) bool { return status.Code(err) == codes.Unauthenticated }
