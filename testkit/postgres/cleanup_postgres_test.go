package postgres_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weiloon1234/Foundry-Go/database"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func TestNamespaceCleanup(t *testing.T) {
	admin := pgtest.Open(t)
	other := pgtest.Isolate(t)
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		mode     string
		failed   bool
		retained bool
		keep     bool
	}{
		{mode: "scope"},
		{mode: "namespace"},
		{mode: "prepared"},
		{mode: "uncreated"},
		{mode: "failure", failed: true, retained: true},
		{mode: "namespace_failure", failed: true, retained: true},
		{mode: "panic", failed: true, retained: true},
		{mode: "panic_nil", failed: true, retained: true},
		{mode: "goexit", failed: true, retained: true},
		{mode: "subtest", failed: true, retained: true},
		{mode: "parallel_subtest", failed: true, retained: true},
		{mode: "cleanup_failure", failed: true, retained: true},
		{mode: "cleanup_panic", failed: true, retained: true},
		{mode: "keep", retained: true, keep: true},
		{mode: "namespace_keep", retained: true, keep: true},
		{mode: "idle"},
		{mode: "locked", retained: true},
	} {
		mode := test.mode
		t.Run(mode, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "schema")
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, binary, "-test.run=^TestNamespaceCleanupChild$", "-test.v")
			command.WaitDelay = time.Second
			command.Env = append(os.Environ(), "FOUNDRY_SCHEMA_TEST_MODE="+mode, "FOUNDRY_SCHEMA_TEST_MARKER="+marker, "FOUNDRY_TEST_KEEP_SCHEMAS=0")
			if test.keep {
				command.Env = append(command.Env, "FOUNDRY_TEST_KEEP_SCHEMAS=1")
			}
			output, err := command.CombinedOutput()
			if ctx.Err() != nil || (err != nil) != test.failed {
				t.Fatalf("child outcome: %v\n%s", err, output)
			}
			data, err := os.ReadFile(marker)
			if err != nil {
				t.Fatal(err)
			}
			schema := string(data)
			if !strings.Contains(string(output), "Retained PostgreSQL namespace: "+schema) {
				t.Fatalf("namespace inspection log missing:\n%s", output)
			}
			if mode == "locked" && !strings.Contains(string(output), "could not drop PostgreSQL test namespace") {
				t.Fatalf("blocked drop was not logged:\n%s", output)
			}
			for name, want := range map[string]bool{schema: test.retained, other.Schema(): true} {
				var exists bool
				if err := database.ScanOne(t.Context(), admin, "SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_namespace WHERE nspname=$1)", []any{name}, &exists); err != nil {
					t.Fatal(err)
				}
				if exists != want {
					t.Errorf("schema %s exists=%v, want %v", name, exists, want)
				}
			}
		})
	}
}

func TestNamespaceCleanupChild(t *testing.T) {
	mode := os.Getenv("FOUNDRY_SCHEMA_TEST_MODE")
	if mode == "" {
		return
	}
	var schema string
	if strings.HasPrefix(mode, "namespace") {
		db := pgtest.Open(t)
		t.Cleanup(func() {
			if db.Stats().Open != 0 {
				t.Error("namespace cleanup ran before its creation pool closed")
			}
		})
		schema = pgtest.Namespace(t, db)
	} else if mode == "prepared" || mode == "uncreated" {
		name, create := pgtest.PrepareNamespace(t)
		schema = name
		if mode == "prepared" {
			create(pgtest.Open(t))
		}
		t.Logf("Retained PostgreSQL namespace: %s", schema)
	} else {
		scope := pgtest.Isolate(t)
		schema = scope.Schema()
		db := scope.Open(t)
		if _, err := db.Exec(t.Context(), "CREATE TABLE records(id bigint)"); err != nil {
			t.Fatal(err)
		}
		if mode == "locked" || mode == "idle" {
			conn, err := pgx.Connect(t.Context(), os.Getenv(pgtest.URLVariable))
			if err != nil {
				t.Fatal("connect lock holder failed")
			}
			// Deliberately leave this connection and transaction open through
			// test cleanup. Process exit releases them after the bounded drop.
			if mode == "locked" {
				if _, err := conn.Exec(t.Context(), `BEGIN; LOCK TABLE "`+schema+`".records IN ACCESS SHARE MODE`); err != nil {
					t.Fatal("lock namespace table failed")
				}
			}
		}
	}
	if err := os.WriteFile(os.Getenv("FOUNDRY_SCHEMA_TEST_MARKER"), []byte(schema), 0600); err != nil {
		t.Fatal(err)
	}
	switch mode {
	case "failure", "namespace_failure":
		t.Fatal("intentional schema owner failure")
	case "panic":
		panic("intentional schema owner panic")
	case "panic_nil":
		panic(nil)
	case "goexit":
		runtime.Goexit()
	case "subtest":
		t.Run("failed", func(t *testing.T) { t.Fatal("intentional subtest failure") })
	case "parallel_subtest":
		t.Run("failed", func(t *testing.T) { t.Parallel(); t.Fatal("intentional parallel subtest failure") })
	case "cleanup_failure":
		t.Cleanup(func() { t.Error("intentional connection cleanup failure") })
	case "cleanup_panic":
		t.Cleanup(func() { panic("intentional cleanup panic") })
	}
}
