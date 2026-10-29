package goway_test

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"

	j "foundry.test/goway/models/aggregates"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

// This exercises compilation, driver calls and hydration, without PostgreSQL I/O.
func BenchmarkRelationAggregateSlots1000(b *testing.B) {
	const parents, batchSize = 1000, 500
	parentRows := make([][]driver.Value, parents)
	for i := range parentRows {
		parentRows[i] = []driver.Value{int64(i + 1), "parent", nil}
	}
	g := &goldenCapture{quiet: true}
	var root, direct, pivot int
	var directKeys, pivotKeys [parents]bool
	g.reply = func(sql string, args []driver.NamedValue) ([]string, [][]driver.Value) {
		if !strings.Contains(sql, "GROUP BY") {
			root++
			return make([]string, 3), parentRows
		}
		keys := aggregateBatchKeys(args)
		if len(keys) != batchSize {
			b.Fatalf("batch keys = %d, want %d", len(keys), batchSize)
		}
		columns, seen, value := 2, &directKeys, int64(2)
		if strings.Contains(sql, `AS "foundry_pivot"`) {
			pivot++
			columns, seen, value = 4, &pivotKeys, 3
		} else {
			direct++
		}
		rows := make([][]driver.Value, len(keys))
		for i, key := range keys {
			if key < 1 || key > parents || seen[key-1] {
				b.Fatalf("invalid or repeated group key %d", key)
			}
			seen[key-1] = true
			rows[i] = []driver.Value{key, value}
			if columns == 4 {
				rows[i] = append(rows[i], value, value)
			}
		}
		return make([]string, columns), rows
	}
	db := g.open(b.Context())
	b.Cleanup(func() {
		if err := db.Close(context.Background()); err != nil {
			b.Error(err)
		}
	})
	q := orm.WithRelationLimits(j.Parents.Query().Limit(parents).With(j.ParentAggregates.Count, j.ParentAggregates.Pivots), orm.RelationLimits{BatchSize: batchSize, MaxRows: parents * 2, MaxDepth: 2})
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		root, direct, pivot = 0, 0, 0
		directKeys, pivotKeys = [parents]bool{}, [parents]bool{}
		before := g.statements
		out, err := orm.All(b.Context(), db, q)
		if err != nil {
			b.Fatal(err)
		}
		if len(out) != parents {
			b.Fatalf("parents = %d, want %d", len(out), parents)
		}
		if root != 1 || direct != 2 || pivot != 2 || g.statements-before != 5 {
			b.Fatalf("statements root/direct/pivot/total = %d/%d/%d/%d, want 1/2/2/5", root, direct, pivot, g.statements-before)
		}
		for i, p := range out {
			count, countLoaded := p.Count.Get()
			pivots, pivotLoaded := p.OtherCount.Get()
			if p.ID != int64(i+1) || !countLoaded || count != 2 || !pivotLoaded || pivots != 3 || !directKeys[i] || !pivotKeys[i] {
				b.Fatalf("incorrect aggregate hydration for parent %d", i+1)
			}
		}
	}
	b.ReportMetric(float64(g.statements)/float64(b.N), "statements/op")
}
