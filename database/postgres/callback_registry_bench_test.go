package postgres_test

import (
	"bytes"
	"context"
	"errors"
	pgx "github.com/jackc/pgx/v5"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

const rewriteBoundarySQL = "SELECT $1::pg_catalog.int8"
const rewriteProtocolSamples = 8

type rewriteBoundaryProbe struct{ native **pgx.Conn }

func (r rewriteBoundaryProbe) RewriteQuery(_ context.Context, conn *pgx.Conn, query string, args []any) (string, []any, error) {
	if r.native != nil {
		*r.native = conn
	}
	return query, args, nil
}

type realBoundaryRewrite struct{}

func (realBoundaryRewrite) RewriteQuery(_ context.Context, _ *pgx.Conn, _ string, args []any) (string, []any, error) {
	if len(args) != 1 {
		return "", nil, errors.New("benchmark rewrite argument count changed")
	}
	value, ok := args[0].(int64)
	if !ok {
		return "", nil, errors.New("benchmark rewrite argument type changed")
	}
	return "SELECT ($1::pg_catalog.int8 - 1)", []any{value + 1}, nil
}

// Protocol tracing starts after authentication, counts only message names, and
// discards payloads. It is disabled during timing/allocation measurement.
type rewriteProtocolCounts struct{ statements, cycles atomic.Uint64 }

func (c *rewriteProtocolCounts) Write(data []byte) (int, error) {
	if bytes.HasPrefix(data, []byte("F\tQuery\t")) || bytes.HasPrefix(data, []byte("F\tExecute\t")) {
		c.statements.Add(1)
	}
	if bytes.HasPrefix(data, []byte("B\tReadyForQuery\t")) {
		c.cycles.Add(1)
	}
	return len(data), nil
}
func rewriteBenchmarkPool(tb testing.TB, options ...database.Option) (*database.DB, *pgx.Conn) {
	tb.Helper()
	config := pgtest.Config(tb)
	config.Schema = ""
	config.Pool.MaxOpen = 1
	config.Pool.MaxIdle = 1
	db, err := postgres.Open(tb.Context(), config, options...)
	if err != nil {
		tb.Fatal("open rewrite measurement pool failed")
	}
	tb.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := db.Close(ctx); err != nil {
			tb.Error("close rewrite measurement pool failed")
		}
	})
	var native *pgx.Conn
	var value int64
	if err := database.ScanOne(tb.Context(), db, rewriteBoundarySQL, []any{rewriteBoundaryProbe{native: &native}, int64(42)}, &value); err != nil || native == nil || value != 42 {
		tb.Fatalf("rewrite benchmark warmup failed: err=%v result=%d native=%t", err, value, native != nil)
	}
	return db, native
}
func measureRewriteProtocol(tb testing.TB, db *database.DB, native *pgx.Conn, args []any) (uint64, uint64) {
	tb.Helper()
	var value int64
	if err := database.ScanOne(tb.Context(), db, rewriteBoundarySQL, args, &value); err != nil || value != 42 {
		tb.Fatalf("rewrite protocol warmup failed: err=%v result=%d", err, value)
	}
	var counts rewriteProtocolCounts
	frontend := native.PgConn().Frontend()
	frontend.Trace(&counts, pgproto3.TracerOptions{SuppressTimestamps: true, RegressMode: true})
	defer frontend.Untrace()
	for range rewriteProtocolSamples {
		value = 0
		if err := database.ScanOne(tb.Context(), db, rewriteBoundarySQL, args, &value); err != nil || value != 42 {
			tb.Fatalf("rewrite protocol sample failed: err=%v result=%d", err, value)
		}
	}
	statements, cycles := counts.statements.Load(), counts.cycles.Load()
	if statements == 0 || cycles == 0 || statements%rewriteProtocolSamples != 0 || cycles%rewriteProtocolSamples != 0 {
		tb.Fatal("rewrite protocol counts are incomplete or unstable")
	}
	return statements / rewriteProtocolSamples, cycles / rewriteProtocolSamples
}

// Run this same benchmark before and after changing registry protection. Report
// protocol counts separately from timing: tracing is excluded from the timed
// region, every iteration reads and validates the server's result, and connection
// creation, authentication, preparation and initial type adaptation are warmup.
// Scope: sequential statements on one warm, unscoped connection.
func BenchmarkPostgresRewriteBoundary(b *testing.B) {
	cases := []struct {
		name    string
		rewrite pgx.QueryRewriter
	}{
		{name: "Ordinary"},
		{name: "NoopCustomRewrite", rewrite: rewriteBoundaryProbe{}},
		{name: "RealCustomRewrite", rewrite: realBoundaryRewrite{}},
	}
	for _, test := range cases {
		b.Run(test.name, func(b *testing.B) {
			db, native := rewriteBenchmarkPool(b)
			args := []any{int64(42)}
			if test.rewrite != nil {
				args = []any{test.rewrite, int64(42)}
			}
			statements, cycles := measureRewriteProtocol(b, db, native, args)
			b.ReportAllocs()
			var value int64
			for b.Loop() {
				value = 0
				if err := database.ScanOne(b.Context(), db, rewriteBoundarySQL, args, &value); err != nil || value != 42 {
					b.Fatal("rewrite benchmark operation failed")
				}
			}
			b.ReportMetric(float64(statements), "server-statements/op")
			b.ReportMetric(float64(cycles), "protocol-cycles/op")
		})
	}
}
func TestPostgresNoopRewriteAddsNoProtocolCycle(t *testing.T) {
	var observed atomic.Uint64
	db, native := rewriteBenchmarkPool(t, database.WithQueryObserver(func(context.Context, database.QueryEvent) { observed.Add(1) }))
	for _, args := range [][]any{{int64(42)}, {rewriteBoundaryProbe{}, int64(42)}} {
		before := observed.Load()
		statements, cycles := measureRewriteProtocol(t, db, native, args)
		if observed.Load()-before != rewriteProtocolSamples+1 {
			t.Fatal("Foundry observer count differs from executed statements")
		}
		if statements != 1 || cycles != 1 {
			t.Fatal("no-op rewrite added a server statement or protocol cycle")
		}
	}
}
