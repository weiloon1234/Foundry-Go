package postgres

import (
	"context"
	"crypto/rand"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weiloon1234/Foundry-Go/database"
)

func TestDropNamespaceWaitsForAdvisoryLock(t *testing.T) {
	config := Config(t)
	db := Open(t)
	schema := namespacePrefix + strings.ToLower(rand.Text())
	if _, err := db.Exec(t.Context(), `CREATE SCHEMA "`+schema+`"`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
		defer cancel()
		if _, err := db.Exec(ctx, `DROP SCHEMA IF EXISTS "`+schema+`" CASCADE`); err != nil {
			t.Errorf("clean up advisory lock test schema: %v", err)
		}
	})

	conn, err := pgx.Connect(t.Context(), os.Getenv(URLVariable))
	if err != nil {
		t.Fatal("connect advisory lock holder failed")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
		defer cancel()
		if err := conn.Close(ctx); err != nil {
			t.Error("close advisory lock holder failed")
		}
	})
	if _, err := conn.Exec(t.Context(), "SELECT pg_catalog.pg_advisory_lock($1)", dropLockKey); err != nil {
		t.Fatal("acquire namespace cleanup advisory lock failed")
	}

	done := make(chan struct{})
	var dropErr error
	go func() {
		defer close(done)
		dropErr = dropNamespace(config, schema)
	}()
	// Release the holder before waiting on failure paths, and drain the drop
	// before the schema and pool cleanups run.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
		defer cancel()
		if err := conn.Close(ctx); err != nil {
			t.Error("release advisory lock holder failed")
		}
		select {
		case <-done:
		case <-time.After(dropQueueTimeout + cleanupTimeout):
			t.Error("namespace drop did not finish during cleanup")
		}
	})

	select {
	case <-done:
		t.Fatalf("namespace drop finished while advisory lock was held: %v", dropErr)
	case <-time.After(300 * time.Millisecond):
	}
	var exists bool
	if err := database.ScanOne(t.Context(), db, "SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_namespace WHERE nspname=$1)", []any{schema}, &exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("namespace was removed while advisory lock was held")
	}
	var released bool
	if err := conn.QueryRow(t.Context(), "SELECT pg_catalog.pg_advisory_unlock($1)", dropLockKey).Scan(&released); err != nil || !released {
		t.Fatal("release namespace cleanup advisory lock failed")
	}
	select {
	case <-done:
		if dropErr != nil {
			t.Fatalf("namespace drop after advisory lock release: %v", dropErr)
		}
	case <-time.After(dropQueueTimeout + cleanupTimeout):
		t.Fatal("namespace drop did not finish after advisory lock release")
	}
	if err := database.ScanOne(t.Context(), db, "SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_namespace WHERE nspname=$1)", []any{schema}, &exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("namespace still exists after successful drop")
	}
}
