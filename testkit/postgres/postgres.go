// Package postgres provides opt-in PostgreSQL acceptance helpers. Callers supply
// only their isolated test database. Passing tests remove only their own schemas;
// failed tests retain them for inspection. No helper resets or drops a database.
package postgres

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
	"github.com/weiloon1234/Foundry-Go/secret"
)

const (
	URLVariable         = "FOUNDRY_TEST_POSTGRES_URL"
	RequiredVariable    = "FOUNDRY_TEST_POSTGRES_REQUIRED"
	KeepSchemasVariable = "FOUNDRY_TEST_KEEP_SCHEMAS"
)

const namespacePrefix = "foundry_test_"

const dropQueueTimeout = 2 * time.Minute

// dropLockKey serialises schema drops across test processes in each database
// until commit frees their slots in PostgreSQL's server-wide shared lock table.
const dropLockKey int64 = 0x466f756e64727944

// KeepSchemas reports whether test-owned PostgreSQL artifacts must be retained.
// Call it during cleanup so subtest and already-run cleanup failures are included.
// A cleanup registered before the namespace runs after its drop and cannot retain it.
func KeepSchemas(t testing.TB) bool {
	t.Helper()
	if t.Failed() || os.Getenv(KeepSchemasVariable) == "1" {
		return true
	}
	// testing runs cleanups while unwinding a panic, before marking the test
	// failed. Inspect function names only; never recover or format its payload.
	// An incomplete stack cannot establish success, so retain conservatively.
	var pcs [128]uintptr
	n := runtime.Callers(2, pcs[:])
	if n == len(pcs) {
		return true
	}
	frames := runtime.CallersFrames(pcs[:n])
	for {
		frame, more := frames.Next()
		if frame.Function == "runtime.gopanic" || frame.Function == "runtime.Goexit" {
			return true
		}
		if !more {
			return false
		}
	}
}

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
// Cleanup removes only this schema when the test passes and KeepSchemas is false.
// The caller owns db and its closure. Register application cleanup later so
// LIFO cleanup shuts it down before schema cleanup.
func Namespace(t testing.TB, db *database.DB) string {
	t.Helper()
	name, create := PrepareNamespace(t)
	create(db)
	return name
}

// PrepareNamespace reserves a generated name and registers cleanup before any
// application or pool cleanup. Call create once, with a pool in the configured
// test database, when setup is ready to create the schema. Until then cleanup
// owns nothing. This permits assertions that startup does not create schemas.
func PrepareNamespace(t testing.TB) (name string, create func(*database.DB)) {
	t.Helper()
	config := Config(t)
	name = namespacePrefix + strings.ToLower(rand.Text())
	created := false
	t.Cleanup(func() {
		if !created || KeepSchemas(t) {
			return
		}
		if err := dropNamespace(config, name); err != nil {
			t.Logf("could not drop PostgreSQL test namespace %s; retained for inspection: %v", name, err)
		}
	})
	return name, func(db *database.DB) {
		t.Helper()
		if created {
			t.Fatal("PostgreSQL test namespace already created")
		}
		if _, err := db.Exec(t.Context(), `CREATE SCHEMA "`+name+`"`); err != nil {
			t.Fatalf("create PostgreSQL test namespace: %v", err)
		}
		created = true
		t.Logf("Retained PostgreSQL namespace: %s", name)
	}
}

func dropNamespace(config postgres.Config, name string) error {
	if !strings.HasPrefix(name, namespacePrefix) || len(name) <= len(namespacePrefix) || !sqlname.Valid(name) {
		return errors.New("refusing to drop a non-generated PostgreSQL test namespace")
	}
	ctx, cancel := context.WithTimeout(context.Background(), dropQueueTimeout+cleanupTimeout)
	defer cancel()
	// Use an independent pool: the test's own pool may already have closed,
	// changed its search path, or retained a connection holding a table lock.
	config.Schema = ""
	config.Pool.MaxOpen, config.Pool.MaxIdle = 1, 1
	config.Pool.CommitTimeout = cleanupTimeout
	config.StatementTimeout = cleanupTimeout
	config.LockTimeout = 100 * time.Millisecond
	db, err := postgres.Open(ctx, config)
	if err != nil {
		return err
	}
	defer closePool(db)
	return db.Transaction(ctx, func(tx *database.Tx) error {
		wait, cancel := context.WithTimeout(ctx, dropQueueTimeout)
		defer cancel()
		for {
			var acquired bool
			if err := database.ScanOne(wait, tx, "SELECT pg_catalog.pg_try_advisory_xact_lock($1)", []any{dropLockKey}, &acquired); err != nil {
				return err
			}
			if acquired {
				break
			}
			timer := time.NewTimer(50 * time.Millisecond)
			select {
			case <-timer.C:
			case <-wait.Done():
				timer.Stop()
				return wait.Err()
			}
		}
		// Resolve schema-less parts (such as view rules) and TOAST through their
		// automatic/internal owner. Unknown ownership conservatively keeps the schema.
		var outside bool
		if err := database.ScanOne(ctx, tx, `SELECT EXISTS (
			SELECT 1 FROM pg_catalog.pg_depend d
			CROSS JOIN LATERAL pg_catalog.pg_identify_object(d.refclassid, d.refobjid, d.refobjsubid) referenced
			CROSS JOIN LATERAL pg_catalog.pg_identify_object(d.classid, d.objid, d.objsubid) dependent
			LEFT JOIN pg_catalog.pg_depend ownership ON ownership.classid=d.classid AND ownership.objid=d.objid
				AND ownership.deptype IN ('a', 'i') AND (dependent.schema IS NULL OR dependent.schema='pg_toast')
			LEFT JOIN LATERAL pg_catalog.pg_identify_object(ownership.refclassid, ownership.refobjid, ownership.refobjsubid) owner ON true
			WHERE (referenced.schema=$1 OR (d.refclassid='pg_catalog.pg_namespace'::regclass
				AND d.refobjid=(SELECT oid FROM pg_catalog.pg_namespace WHERE nspname=$1)))
			AND COALESCE(NULLIF(dependent.schema, 'pg_toast'), owner.schema) IS DISTINCT FROM $1
		)`, []any{name}, &outside); err != nil {
			return err
		}
		if outside {
			return errors.New("outside dependency prevents PostgreSQL test namespace cleanup")
		}
		// Random schema names make concurrent dependency changes vanishingly unlikely
		// in tests; protecting against them is out of scope.
		_, err := tx.Exec(ctx, `DROP SCHEMA "`+name+`" CASCADE`)
		return err
	})
}
