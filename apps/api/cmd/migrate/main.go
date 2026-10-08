// Command migrate applies, reverts and verifies IdeaVault database migrations.
//
//	migrate up | down [steps] | status | verify
//
// verify applies all migrations, rolls every one back and re-applies them, failing on
// any error — used in CI to prove migrations are reversible.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/config"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/db"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/observability"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cmd := "up"
	if len(args) > 0 {
		cmd = args[0]
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := observability.NewLogger(os.Stderr, "info", "text")
	ctx := context.Background()
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	dir, err := db.FindMigrationsDir(cfg.MigrationsDir)
	if err != nil {
		return err
	}
	migs, err := db.LoadMigrations(dir)
	if err != nil {
		return err
	}
	switch cmd {
	case "up":
		n, err := db.MigrateUp(ctx, pool, migs, log)
		fmt.Printf("applied %d migration(s)\n", n)
		return err
	case "down":
		steps := 1
		if len(args) > 1 {
			if steps, err = strconv.Atoi(args[1]); err != nil {
				return fmt.Errorf("steps must be a number")
			}
		}
		n, err := db.MigrateDown(ctx, pool, migs, steps, log)
		fmt.Printf("reverted %d migration(s)\n", n)
		return err
	case "status":
		st, err := db.Status(ctx, pool, migs)
		if err != nil {
			return err
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(st)
	case "verify":
		if _, err := db.MigrateUp(ctx, pool, migs, log); err != nil {
			return fmt.Errorf("up: %w", err)
		}
		if _, err := db.MigrateDown(ctx, pool, migs, len(migs), log); err != nil {
			return fmt.Errorf("down: %w", err)
		}
		if _, err := db.MigrateUp(ctx, pool, migs, log); err != nil {
			return fmt.Errorf("re-up: %w", err)
		}
		st, err := db.Status(ctx, pool, migs)
		if err != nil {
			return err
		}
		for _, s := range st {
			if !s.Applied || s.Drift {
				return fmt.Errorf("migration %04d_%s not cleanly applied", s.Version, s.Name)
			}
		}
		fmt.Printf("verified %d migration(s): up → down → up OK\n", len(migs))
		return nil
	}
	return fmt.Errorf("unknown command %q (use up, down, status, verify)", cmd)
}
