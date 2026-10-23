package database_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

func TestStatementDetectorPoolTransactionAndComposition(t *testing.T) {
	d, err := database.NewStatementDetector(database.StatementDetectorConfig{Threshold: 2, MaxShapes: 8})
	if err != nil {
		t.Fatal(err)
	}
	for _, reverse := range []bool{false, true} {
		var observed atomic.Int64
		opts := []database.Option{database.WithStatementDetector(d), database.WithQueryObserver(func(context.Context, database.QueryEvent) { observed.Add(1); panic("observer failure") })}
		if reverse {
			opts[0], opts[1] = opts[1], opts[0]
		}
		db, err := database.Open(t.Context(), database.Adapter{Connector: connector{&driverState{}}}, database.DefaultPoolConfig(), opts...)
		if err != nil {
			t.Fatal(err)
		}
		ctx, scope := d.Begin(t.Context())
		for range 2 {
			if _, err = db.Exec(ctx, "UPDATE users SET name=$1", "canary"); err != nil {
				t.Fatal(err)
			}
		}
		err = db.Transaction(ctx, func(tx *database.Tx) error { _, err := tx.Exec(ctx, "UPDATE users SET name=$1", "private"); return err })
		if err != nil {
			t.Fatal(err)
		}
		report := scope.Report()
		if len(report.Violations) != 1 || report.Violations[0].Count != 3 || observed.Load() != 3 {
			t.Fatal(report, observed.Load())
		}
		if !strings.HasPrefix(report.Violations[0].Shape, "untrusted statement ") {
			t.Fatal(report)
		}
		if err = db.Close(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
}
func TestStatementDetectorScopesAndBounds(t *testing.T) {
	d, _ := database.NewStatementDetector(database.StatementDetectorConfig{Threshold: 1, MaxShapes: 1})
	other, _ := database.NewStatementDetector(database.StatementDetectorConfig{Threshold: 1, MaxShapes: 1})
	var wg sync.WaitGroup
	scopes := make([]*database.StatementScope, 20)
	for i := range scopes {
		ctx, s := d.Begin(t.Context())
		scopes[i] = s
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 2 {
				d.Observe(ctx, database.QueryEvent{Statement: "select 'canary'"})
			}
			other.Observe(ctx, database.QueryEvent{Statement: "select 'other'"})
		}()
	}
	wg.Wait()
	for _, s := range scopes {
		r := s.Report()
		if len(r.Violations) != 1 || r.Violations[0].Count != 2 {
			t.Fatal(r)
		}
	}
	ctx, s := d.Begin(t.Context())
	for range 10 {
		wg.Add(1)
		go func() { defer wg.Done(); d.Observe(ctx, database.QueryEvent{Statement: "select 1"}) }()
	}
	wg.Wait()
	if s.Report().Violations[0].Count != 10 {
		t.Fatal(s.Report())
	}
	d.Observe(ctx, database.QueryEvent{Statement: "select id from users"})
	d.Observe(ctx, database.QueryEvent{Statement: "select 'unterminated"})
	if r := s.Report(); !r.Overflow {
		t.Fatal(r)
	}
	r := s.Report()
	r.Violations[0].Count = 0
	if s.Report().Violations[0].Count != 10 {
		t.Fatal("report aliases counter")
	}
}
func TestStatementDetectorCancellation(t *testing.T) {
	d, _ := database.NewStatementDetector(database.StatementDetectorConfig{Threshold: 1, MaxShapes: 8})
	ctx, scope := d.Begin(t.Context())
	ctx, cancel := context.WithCancel(ctx)
	state := &driverState{exec: func(context.Context, string, []driver.NamedValue) (driver.Result, error) {
		cancel()
		return nil, context.Canceled
	}}
	db, err := database.Open(t.Context(), database.Adapter{Connector: connector{state}}, database.DefaultPoolConfig(), database.WithStatementDetector(d))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(t.Context())
	if _, err = db.Exec(ctx, "select 'private'"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	// Completion still belongs to the canceled scope; reporting does not use SQL args.
	d.Observe(ctx, database.QueryEvent{Statement: "select $1"})
	if r := scope.Report(); len(r.Violations) != 1 || r.Violations[0].Count != 2 {
		t.Fatal(r)
	}
}
func BenchmarkStatementDetectorPaths(b *testing.B) {
	for _, name := range []string{"disabled_nil_instrumentation", "disabled_not_installed", "enabled_untrusted", "enabled_trusted"} {
		b.Run(name, func(b *testing.B) {
			var opts []database.Option // Both disabled cases use exactly the same nil path.
			ctx := b.Context()
			var scope *database.StatementScope
			if strings.HasPrefix(name, "enabled_") {
				d, err := database.NewStatementDetector(database.DefaultStatementDetectorConfig())
				if err != nil {
					b.Fatal(err)
				}
				opts = append(opts, database.WithStatementDetector(d))
				ctx, scope = d.Begin(ctx)
				if name == "enabled_trusted" {
					c := querycore.NewCompiler(nil)
					c.TrackShape()
					parameter, e := c.Parameter(int64(1))
					if e != nil {
						b.Fatal(e)
					}
					statement := c.Statement("UPDATE records SET value=" + parameter)
					ctx = statement.Context(ctx)
				}
			}
			var calls atomic.Int64
			state := &driverState{exec: func(context.Context, string, []driver.NamedValue) (driver.Result, error) {
				calls.Add(1)
				return driver.RowsAffected(1), nil
			}}
			db, err := database.Open(b.Context(), database.Adapter{Connector: connector{state}}, database.DefaultPoolConfig(), opts...)
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = db.Close(context.Background()) })
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := db.Exec(ctx, "UPDATE records SET value=$1", int64(1)); err != nil {
					b.Fatal(err)
				}
			}
			if calls.Load() != int64(b.N) {
				b.Fatal("statements not executed", calls.Load(), b.N)
			}
			if scope != nil {
				r := scope.Report()
				if r.Overflow || len(r.Violations) != 1 || r.Violations[0].Count != b.N {
					b.Fatal("detector did not count timed work", r, b.N)
				}
			}
			b.ReportMetric(1, "statements/op")
		})
	}
}

func TestStatementDetectorExcludesFrameworkSavepoints(t *testing.T) {
	d, _ := database.NewStatementDetector(database.StatementDetectorConfig{Threshold: 1, MaxShapes: 1})
	ctx, scope := d.Begin(t.Context())
	var observed []database.QueryEvent
	db, err := database.Open(ctx, database.Adapter{Connector: connector{&driverState{}}}, database.DefaultPoolConfig(), database.WithStatementDetector(d), database.WithQueryObserver(func(_ context.Context, e database.QueryEvent) { observed = append(observed, e) }))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(t.Context())
	rollback := errors.New("rollback child")
	err = db.Transaction(ctx, func(tx *database.Tx) error {
		for i := 0; i < 200; i++ {
			err := tx.Savepoint(ctx, func(*database.Tx) error {
				if i%2 == 0 {
					return rollback
				}
				return nil
			})
			if i%2 == 0 {
				if !errors.Is(err, rollback) {
					return fmt.Errorf("savepoint rollback: %w", err)
				}
			} else if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	r := scope.Report()
	if r.Overflow || len(r.Violations) != 0 {
		t.Fatal(r)
	}
	// 200 creates + 200 releases + 100 rollback-to statements. Begin/commit
	// use driver methods, which never generated QueryObserver events.
	if len(observed) != 500 {
		t.Fatalf("observer saw %d events", len(observed))
	}
	at := 0
	for i := 1; i <= 200; i++ {
		statements := []string{fmt.Sprintf("SAVEPOINT foundry_sp_%d", i)}
		if i%2 == 1 {
			statements = append(statements, fmt.Sprintf("ROLLBACK TO SAVEPOINT foundry_sp_%d", i))
		}
		statements = append(statements, fmt.Sprintf("RELEASE SAVEPOINT foundry_sp_%d", i))
		for _, want := range statements {
			e := observed[at]
			at++
			if e.Statement != want || e.Operation != "exec" || e.Code != "" || e.Role != database.PrimaryPool {
				t.Fatal(e, want)
			}
		}
	}
	// An application-issued command is still counted: exclusion uses framework
	// provenance, never keyword matching. With capacity one this also proves
	// no framework control statement retained a hidden below-threshold shape.
	for range 2 {
		if _, err = db.Exec(ctx, "ROLLBACK"); err != nil {
			t.Fatal(err)
		}
	}
	r = scope.Report()
	if r.Overflow || len(r.Violations) != 1 || r.Violations[0].Count != 2 || !strings.HasPrefix(r.Violations[0].Shape, "untrusted statement ") {
		t.Fatal(r)
	}
}

func TestStatementReportFingerprintsAreInstanceOwned(t *testing.T) {
	var shapes []string
	for range 2 {
		d, err := database.NewStatementDetector(database.StatementDetectorConfig{Threshold: 1, MaxShapes: 8})
		if err != nil {
			t.Fatal(err)
		}
		ctx, scope := d.Begin(t.Context())
		for _, sql := range []string{"SELECT 'canary-first'", "SELECT 'canary-second'"} {
			d.Observe(ctx, database.QueryEvent{Statement: sql})
		}
		r := scope.Report()
		if len(r.Violations) != 1 || r.Violations[0].Count != 2 || !strings.HasPrefix(r.Violations[0].Shape, "untrusted statement ") || strings.Contains(r.Violations[0].Shape, "canary") {
			t.Fatal(r)
		}
		shapes = append(shapes, r.Violations[0].Shape)
	}
	if shapes[0] == shapes[1] {
		t.Fatal("different detector keys shared a report fingerprint")
	}
}
