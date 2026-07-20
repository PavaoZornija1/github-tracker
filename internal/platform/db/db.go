package db

import (
	"context"
	"database/sql"
	"fmt"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"

	"github.com/PavaoZornija1/github-tracker/internal/ent"

	_ "github.com/lib/pq"
)

// OpenOptions controls startup migration behavior.
type OpenOptions struct {
	// AutoMigrate runs Ent Schema.Create when true (local/dev/test only).
	AutoMigrate bool
}

// OpenPostgres opens an Ent client for Postgres.
// Schema.Create runs only when opts.AutoMigrate is true.
func OpenPostgres(ctx context.Context, databaseURL string, opts OpenOptions) (*ent.Client, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	drv := entsql.OpenDB(dialect.Postgres, db)
	client := ent.NewClient(ent.Driver(drv))
	if opts.AutoMigrate {
		if err := client.Schema.Create(ctx); err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("migrate schema: %w", err)
		}
	}
	return client, nil
}
