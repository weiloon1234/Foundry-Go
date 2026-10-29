package goway_test

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"
	"database/sql/driver"
	"fmt"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"strings"
	"testing"
)

func TestStatementDetectorExternalSitesAndBatchLengths(t *testing.T) {
	d, err := database.NewStatementDetector(database.StatementDetectorConfig{Threshold: 1, MaxShapes: 16})
	if err != nil {
		t.Fatal(err)
	}
	g := &goldenCapture{detector: d}
	db := g.open(t.Context())
	defer db.Close(context.Background())
	ctx, scope := d.Begin(t.Context())
	for range 3 {
		if _, err := db.Exec(ctx, "SET app.token TO canary_private"); err != nil {
			t.Fatal(err)
		}
	}
	r := scope.Report()
	if len(r.Violations) != 1 || r.Violations[0].Count != 3 || !strings.HasPrefix(r.Violations[0].Shape, "untrusted statement ") || !strings.Contains(r.Violations[0].CallSites[0], "statement_detector_behavior_test.go:") {
		t.Fatalf("%+v", r)
	}
	ctx, scope = d.Begin(t.Context())
	for _, n := range []int{1, 3} {
		batch := make([][]driver.Value, n)
		for i := range batch {
			batch[i] = []driver.Value{orderID, fmt.Sprintf("01890f47-1a00-7000-8000-%012d", i+2), int64(500)}
		}
		g.chunkBatches = [][][]driver.Value{batch}
		parents, err := orm.All(ctx, db, models.Orders.Query().With(models.OrderRelations.Buyer))
		if err != nil || len(parents) != n {
			t.Fatalf("parents=%d: %v", len(parents), err)
		}
	}
	r = scope.Report()
	if len(r.Violations) != 2 || r.Overflow {
		t.Fatalf("expected parent+batched child shapes: %+v", r)
	}
	for _, v := range r.Violations {
		if v.Count != 2 || !strings.HasPrefix(v.Shape, "SELECT ") || !strings.Contains(v.CallSites[0], "statement_detector_behavior_test.go:") {
			t.Fatalf("%+v", v)
		}
	}
}

// Measure the actual consumer call site, including context transport, compilation
// and hydration. Root benchmarks separately isolate the execution funnel cost.
func BenchmarkStatementDetectorConsumer(b *testing.B) {
	for _, name := range []string{"compiled_disabled", "compiled_enabled", "raw_disabled", "raw_enabled"} {
		b.Run(name, func(b *testing.B) {
			g := &goldenCapture{quiet: true}
			ctx := b.Context()
			var scope *database.StatementScope
			if strings.HasSuffix(name, "enabled") {
				d, err := database.NewStatementDetector(database.DefaultStatementDetectorConfig())
				if err != nil {
					b.Fatal(err)
				}
				g.detector = d
				ctx, scope = d.Begin(ctx)
			}
			db := g.open(ctx)
			defer db.Close(context.Background())
			q := models.Orders.Query().Where(orm.NumberGt(models.Orders.TotalCents, 100))
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if strings.HasPrefix(name, "compiled") {
					if _, err := orm.Count(ctx, db, q); err != nil {
						b.Fatal(err)
					}
				} else {
					var count int64
					if err := database.ScanOne(ctx, db, `SELECT COUNT(*) FROM "orders" WHERE "total_cents" > $1`, []any{int64(100)}, &count); err != nil {
						b.Fatal(err)
					}
				}
			}
			if g.statements != b.N {
				b.Fatalf("%d statements, want %d", g.statements, b.N)
			}
			if scope != nil {
				r := scope.Report()
				if len(r.Violations) != 1 || r.Violations[0].Count != b.N || !strings.Contains(r.Violations[0].CallSites[0], "statement_detector_behavior_test.go:") {
					b.Fatalf("%+v", r)
				}
			}
			b.ReportMetric(1, "statements/op")
		})
	}
}
