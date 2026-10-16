// Command identity runs the identity gRPC service: accounts, sessions,
// campuses and student verification.
package main

import (
	"context"
	"embed"
	"io/fs"
	"log"
	"log/slog"
	"time"
	_ "time/tzdata" // campus timezones are validated without relying on the OS

	identityv1 "github.com/yourmate/backend/gen/identity/v1"
	"github.com/yourmate/backend/pkg/auth"
	"github.com/yourmate/backend/pkg/config"
	"github.com/yourmate/backend/pkg/db"
	"github.com/yourmate/backend/pkg/grpcx"
	"github.com/yourmate/backend/pkg/mailer"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

func main() {
	grpcx.SetupLogging("identity", config.Bool("DEBUG", false))
	ctx := context.Background()

	loadSettings()

	pool, err := db.Open(ctx, config.MustString("DATABASE_URL"), "identity", int32(config.Int("DB_MAX_CONNS", 10)))
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	migrations, _ := fs.Sub(migrationFiles, "migrations")
	if err := db.Migrate(ctx, pool, "identity", migrations); err != nil {
		log.Fatal(err)
	}

	signer, err := auth.NewSigner(config.MustString("JWT_SECRET"), config.Duration("ACCESS_TOKEN_TTL", 15*time.Minute))
	if err != nil {
		log.Fatal(err)
	}

	s := &server{
		db:         pool,
		signer:     signer,
		refreshTTL: config.Duration("REFRESH_TOKEN_TTL", 30*24*time.Hour),
		appURL:     config.String("APP_URL", "http://localhost:5190"),
		mail: &mailer.Mailer{
			Host:     config.String("SMTP_HOST", ""),
			Port:     config.Int("SMTP_PORT", 1025),
			Username: config.String("SMTP_USERNAME", ""),
			Password: config.String("SMTP_PASSWORD", ""),
			From:     config.MustString("MAIL_FROM"),
		},
	}

	if err := s.importDirectory(ctx); err != nil {
		log.Fatalf("import campus directory: %v", err)
	}
	if email := config.String("ADMIN_EMAIL", ""); email != "" {
		if err := s.bootstrapAdmin(ctx, email, config.MustString("ADMIN_PASSWORD"), config.MustString("ADMIN_CAMPUS_SLUG")); err != nil {
			log.Fatalf("bootstrap admin: %v", err)
		}
	}

	srv := grpcx.NewServer(config.MustString("INTERNAL_TOKEN"))
	identityv1.RegisterIdentityServiceServer(srv, s)
	if err := grpcx.Serve(srv, config.String("GRPC_ADDR", ":50071")); err != nil {
		slog.Error("server stopped", "err", err)
	}
}
