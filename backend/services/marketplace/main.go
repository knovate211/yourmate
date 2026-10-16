// Command marketplace runs the marketplace gRPC service: provider profiles,
// availability, bookings, messages, reviews and notifications.
package main

import (
	"context"
	"embed"
	"io/fs"
	"log"
	"log/slog"
	"time"
	_ "time/tzdata"

	"github.com/jackc/pgx/v5/pgxpool"

	identityv1 "github.com/yourmate/backend/gen/identity/v1"
	marketplacev1 "github.com/yourmate/backend/gen/marketplace/v1"
	"github.com/yourmate/backend/pkg/config"
	"github.com/yourmate/backend/pkg/db"
	"github.com/yourmate/backend/pkg/grpcx"
	"github.com/yourmate/backend/pkg/mailer"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type server struct {
	marketplacev1.UnimplementedMarketplaceServiceServer
	db       *pgxpool.Pool
	identity identityv1.IdentityServiceClient
	mail     *mailer.Mailer
	appURL   string
	now      func() time.Time
}

func main() {
	grpcx.SetupLogging("marketplace", config.Bool("DEBUG", false))
	ctx := context.Background()

	loadSettings()

	pool, err := db.Open(ctx, config.MustString("DATABASE_URL"), "marketplace", int32(config.Int("DB_MAX_CONNS", 10)))
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	migrations, _ := fs.Sub(migrationFiles, "migrations")
	if err := db.Migrate(ctx, pool, "marketplace", migrations); err != nil {
		log.Fatal(err)
	}

	token := config.MustString("INTERNAL_TOKEN")
	idConn, err := grpcx.Dial(config.String("IDENTITY_ADDR", "localhost:50071"), token)
	if err != nil {
		log.Fatal(err)
	}
	defer idConn.Close()

	s := &server{
		db:       pool,
		identity: identityv1.NewIdentityServiceClient(idConn),
		appURL:   config.String("APP_URL", "http://localhost:5190"),
		now:      time.Now,
		mail: &mailer.Mailer{
			Host:     config.String("SMTP_HOST", ""),
			Port:     config.Int("SMTP_PORT", 1025),
			Username: config.String("SMTP_USERNAME", ""),
			Password: config.String("SMTP_PASSWORD", ""),
			From:     config.MustString("MAIL_FROM"),
		},
	}

	go s.runScheduler(ctx, config.Duration("SCHEDULER_INTERVAL", time.Minute))

	srv := grpcx.NewServer(token)
	marketplacev1.RegisterMarketplaceServiceServer(srv, s)
	if err := grpcx.Serve(srv, config.String("GRPC_ADDR", ":50072")); err != nil {
		slog.Error("server stopped", "err", err)
	}
}
