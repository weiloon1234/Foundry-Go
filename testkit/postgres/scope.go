package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
)

const scopePoolSize = 2
const cleanupTimeout = 5 * time.Second

// Scope is one retained namespace in the explicitly opted-in test database.
// Configuration is immutable; each opened pool is independent and bounded.
type Scope struct{ config postgres.Config }

// Isolate allocates only a fresh schema. No database/server is created, and no
// teardown deletes data. The administration pool closes before this returns.
func Isolate(t testing.TB) *Scope {
	t.Helper()
	c := Config(t)
	c.Pool.MaxOpen, c.Pool.MaxIdle = scopePoolSize, scopePoolSize
	db, err := postgres.Open(t.Context(), c)
	if err != nil {
		t.Fatalf("open namespace administration pool: %v", err)
	}
	defer func() {
		if err := closePool(db); err != nil {
			t.Errorf("close namespace administration pool: %v", err)
		}
	}()
	c.Schema = Namespace(t, db)
	t.Logf("Retained PostgreSQL namespace: %s", c.Schema)
	return &Scope{config: c}
}

func closePool(db *database.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	return db.Close(ctx)
}

func (s *Scope) Schema() string {
	if s == nil {
		return ""
	}
	return s.config.Schema
}

// Open owns a standalone scoped pool for fixtures/assertions. Prefer an app's
// actual database when testing application behavior or transaction ownership.
func (s *Scope) Open(t testing.TB, options ...database.Option) *database.DB {
	t.Helper()
	if s == nil || s.config.Schema == "" {
		t.Fatal("uninitialized PostgreSQL test scope")
	}
	db, err := postgres.Open(t.Context(), s.config, options...)
	if err != nil {
		t.Fatalf("open scoped PostgreSQL pool: %v", err)
	}
	t.Cleanup(func() {
		if err := closePool(db); err != nil {
			t.Errorf("close scoped PostgreSQL pool: %v", err)
		}
	})
	return db
}

// Config returns a caller-owned typed configuration for this retained scope.
// Scope creation uses the explicit URL, so no mutable TLS pointer is shared.
func (s *Scope) Config() postgres.Config {
	if s == nil {
		return postgres.Config{}
	}
	return s.config
}

func (Scope) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("PostgreSQL test scope")) }
