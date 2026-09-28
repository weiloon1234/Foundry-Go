package postgres_test

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"testing"
)

func TestPostgresSchemaEveryConnectionResetAndReconnect(t *testing.T) {
	scope := pgtest.Isolate(t)
	db := scope.Open(t)
	execute(t, db, "CREATE TABLE records(id bigint PRIMARY KEY)")
	execute(t, db, "INSERT INTO records(id) VALUES(1)")
	entered := make(chan int32, 2)
	release := make(chan struct{})
	done := make(chan error, 2)
	for range 2 {
		go func() {
			done <- db.Session(t.Context(), func(session *database.Session) error {
				defer session.Discard()
				var schema string
				var pid int32
				err := database.ScanOne(t.Context(), session, "SELECT current_schema(), pg_backend_pid()", nil, &schema, &pid)
				entered <- pid
				<-release
				if err != nil {
					return err
				}
				if schema != scope.Schema() {
					return errors.New("new connection escaped scope")
				}
				_, err = session.Exec(t.Context(), "SET search_path TO public")
				return err
			})
		}()
	}
	first, second := <-entered, <-entered
	close(release)
	if first == second {
		t.Error("concurrent owners did not acquire distinct connections")
	}
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	var schema, zone string
	var replacement int32
	if err := database.ScanOne(t.Context(), db, "SELECT current_schema(), current_setting('TimeZone'), pg_backend_pid()", nil, &schema, &zone, &replacement); err != nil || schema != scope.Schema() || replacement == first || replacement == second {
		t.Fatal("replacement connection lost scope", err)
	}
	if err := db.Session(t.Context(), func(s *database.Session) error {
		_, err := s.Exec(t.Context(), "SET search_path TO public")
		if err != nil {
			return err
		}
		_, err = s.Exec(t.Context(), "SET TimeZone TO 'Asia/Tokyo'")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := database.ScanOne(t.Context(), db, "SELECT current_schema(),current_setting('TimeZone')", nil, &schema, &zone); err != nil || schema != scope.Schema() || zone != "UTC" {
		t.Fatal("borrowed session settings leaked", err)
	}
	if countRows(t, db, "records") != 1 {
		t.Fatal("prepared/cached reads lost scoped data")
	}
	// Session tables must not shadow a later borrower's ordinary model table.
	if err := db.Session(t.Context(), func(s *database.Session) error {
		_, err := s.Exec(t.Context(), "CREATE TEMP TABLE records(id bigint)")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if countRows(t, db, "records") != 1 {
		t.Fatal("temporary table escaped session ownership")
	}
	if db.Stats().Open > 2 || db.Stats().Owners != 0 {
		t.Fatal("connection bounds/ownership changed")
	}
}

func TestPostgresSchemaMissingFailsAndPublicDoesNotFallback(t *testing.T) {
	scope := pgtest.Isolate(t)
	db := scope.Open(t)
	admin := pgtest.Open(t)
	table := scope.Schema() + "_public"
	execute(t, admin, `CREATE TABLE public."`+table+`"(id bigint)`)
	execute(t, admin, `INSERT INTO public."`+table+`" VALUES(1)`)
	var count int64
	if err := database.ScanOne(t.Context(), db, `SELECT count(*) FROM "`+table+`"`, nil, &count); err == nil {
		t.Fatal("missing scoped table fell back to public")
	}
	c := pgtest.Config(t)
	c.Schema = scope.Schema() + "_missing"
	missing, err := postgres.Open(t.Context(), c)
	if err == nil {
		_ = missing.Close(t.Context())
		t.Fatal("nonexistent schema passed startup")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if opened, err := postgres.Open(ctx, c); err == nil {
		_ = opened.Close(t.Context())
		t.Fatal("canceled scope startup succeeded")
	}
}

// Native opt-in cost check; each sample owns one retained scope and bounded pools.
func BenchmarkPostgresSchemaCheckout(b *testing.B) {
	for _, scoped := range []bool{false, true} {
		name := "Unscoped"
		if scoped {
			name = "Scoped"
		}
		b.Run(name, func(b *testing.B) {
			var db *database.DB
			if scoped {
				db = pgtest.Isolate(b).Open(b)
			} else {
				db = pgtest.Open(b, func(c *postgres.Config) { c.Pool.MaxOpen = 2; c.Pool.MaxIdle = 2 })
			}
			b.ReportAllocs()
			for b.Loop() {
				var value int
				if err := database.ScanOne(b.Context(), db, "SELECT 1", nil, &value); err != nil || value != 1 {
					b.Fatal("native checkout failed", err)
				}
			}
		})
	}
}
