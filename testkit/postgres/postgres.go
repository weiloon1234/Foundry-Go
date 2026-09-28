// Package postgres provides opt-in, non-destructive PostgreSQL acceptance helpers.
// Callers supply only their isolated test database. No helper resets or drops data.
package postgres

import (
	"context"
	"crypto/rand"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	"github.com/weiloon1234/Foundry-Go/secret"
)

const (
	URLVariable      = "FOUNDRY_TEST_POSTGRES_URL"
	RequiredVariable = "FOUNDRY_TEST_POSTGRES_REQUIRED"
)

// Config reads the explicitly opted-in URL. Ordinary test runs skip without it;
// the required acceptance command fails if it is missing. Errors omit credentials.
func Config(t testing.TB) postgres.Config {
	t.Helper()
	value := os.Getenv(URLVariable)
	if value == "" {
		if os.Getenv(RequiredVariable) == "1" {
			t.Fatal("required PostgreSQL acceptance URL is missing")
		}
		t.Skip("PostgreSQL acceptance is opt-in; run make test-postgres")
	}
	config, err := postgres.ParseURL(secret.New(value))
	if err != nil {
		t.Fatalf("PostgreSQL test configuration: %v", err)
	}
	return config
}

// Open owns one test pool and registers bounded cleanup immediately. Optional
// adjustments operate on this test's independent settings before construction.
func Open(t testing.TB, adjust ...func(*postgres.Config)) *database.DB {
	t.Helper()
	config := Config(t)
	for _, fn := range adjust {
		if fn == nil {
			t.Fatal("nil PostgreSQL test configuration adjustment")
		}
		fn(&config)
	}
	db, err := postgres.Open(t.Context(), config)
	if err != nil {
		t.Fatalf("open PostgreSQL test database: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := db.Close(ctx); err != nil {
			t.Errorf("close PostgreSQL test database: %v", err)
		}
	})
	return db
}

// Namespace creates a unique schema in the caller's isolated test database.
// The returned identifier contains only safe ASCII letters, digits and underscores.
// Schemas are retained for inspection; no cleanup runs DROP/TRUNCATE or resets data.
func Namespace(t testing.TB, db *database.DB) string {
	t.Helper()
	name := "foundry_test_" + strings.ToLower(rand.Text())
	if _, err := db.Exec(t.Context(), `CREATE SCHEMA "`+name+`"`); err != nil {
		t.Fatalf("create PostgreSQL test namespace: %v", err)
	}
	return name
}
