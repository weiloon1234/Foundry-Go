package goway_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	j "foundry.test/goway/models/aggregates"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

func aggregateBatchKeys(args []driver.NamedValue) []int64 {
	var keys []int64
	for _, arg := range args {
		if text, ok := arg.Value.(string); ok && strings.HasPrefix(text, "{") {
			for _, part := range strings.Split(strings.Trim(text, "{}"), ",") {
				n, err := strconv.ParseInt(part, 10, 64)
				if err == nil {
					keys = append(keys, n)
				}
			}
		}
	}
	if len(keys) == 0 {
		// The old aggregate contract binds each key separately, followed by LIMIT.
		for _, arg := range args[:max(0, len(args)-1)] {
			if key, ok := arg.Value.(int64); ok {
				keys = append(keys, key)
			}
		}
	}
	return keys
}

func aggregateReply(sql string, args []driver.NamedValue) ([]string, [][]driver.Value) {
	if !strings.Contains(sql, "GROUP BY") {
		if strings.Contains(sql, `FROM "joined_targets"`) {
			return make([]string, 5), [][]driver.Value{{int64(10), int64(1), "ten", int64(7), nil}, {int64(11), int64(1), "eleven", int64(7), nil}, {int64(12), int64(2), "twelve", int64(8), nil}}
		}
		return make([]string, 3), [][]driver.Value{{int64(1), "one", nil}, {int64(2), "two", nil}, {int64(3), "empty", nil}}
	}
	columns := 2
	if strings.Contains(sql, `AS "foundry_pivot"`) {
		columns = 4
	}
	rows := make([][]driver.Value, 0)
	for _, key := range aggregateBatchKeys(args) {
		if key == 3 {
			continue
		}
		n := int64(1)
		if key == 1 {
			n = 2
		}
		var v driver.Value = n
		if strings.Contains(sql, "SUM(") {
			v = strconv.FormatInt(n*7, 10)
		}
		if strings.Contains(sql, "> 0") && !strings.Contains(sql, "FILTER") {
			v = true
		}
		row := []driver.Value{key, v}
		if columns == 4 {
			row = append(row, n, n)
		}
		rows = append(rows, row)
	}
	return make([]string, columns), rows
}

func aggregateDB(t *testing.T, g *goldenCapture) *database.DB {
	t.Helper()
	db := g.open(t.Context())
	t.Cleanup(func() {
		if err := db.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return db
}

func TestRelationAggregateStatementContract(t *testing.T) {
	for _, mode := range []string{"pool", "transaction", "locked"} {
		t.Run(mode, func(t *testing.T) {
			g := &goldenCapture{reply: aggregateReply}
			closed := 0
			g.onClose = func() { closed++ }
			baseReply := g.reply
			g.reply = func(sql string, args []driver.NamedValue) ([]string, [][]driver.Value) {
				if closed != g.statements-1 {
					t.Fatal("previous rows still open")
				}
				return baseReply(sql, args)
			}
			db := aggregateDB(t, g)
			q := orm.WithRelationLimits(j.Parents.Query().With(j.ParentAggregates.Count, j.ParentAggregates.Total), orm.RelationLimits{BatchSize: 2, MaxRows: 100, MaxDepth: 3})
			var parents []j.Parent
			var err error
			if mode == "pool" {
				parents, err = orm.All(t.Context(), db, q)
			} else {
				err = db.Transaction(t.Context(), func(tx *database.Tx) error {
					if mode == "locked" {
						parents, err = orm.AllLocked(t.Context(), tx, orm.Lock(q))
					} else {
						parents, err = orm.All(t.Context(), tx, q)
					}
					return err
				})
			}
			if err != nil {
				t.Fatal(err, g.output.String())
			}
			if g.statements != 5 || closed != 5 || len(parents) != 3 {
				t.Fatal("parent + two batches per slot", g.statements, closed, len(parents))
			}
			for i, p := range parents {
				v, loaded := p.Count.Get()
				want := []int64{2, 1, 0}[i]
				if !loaded || v != want {
					t.Fatal("count", i, v, loaded)
				}
				total, loaded := p.Total.Get()
				if !loaded || i == 2 && !total.IsNull() {
					t.Fatal("nullable loaded result", i, total)
				}
			}
			before := g.statements
			if _, err := orm.LoadMissingFrom(t.Context(), db, j.Parents.Query().With(j.ParentAggregates.Count, j.ParentAggregates.Total), parents); err != nil {
				t.Fatal(err, g.output.String())
			}
			if g.statements != before {
				t.Fatal("loaded zero/NULL queried again")
			}
			parents[0].Count = relation.Value[int64]{}
			loaded, err := orm.LoadMissingFrom(t.Context(), db, j.Parents.Query().With(j.ParentAggregates.Count, j.ParentAggregates.Total), parents)
			if err != nil || g.statements != before+1 || !loaded[0].Count.IsLoaded() || parents[0].Count.IsLoaded() {
				t.Fatal("partial missing/caller ownership", err)
			}
			before = g.statements
			_, err = orm.LoadFrom(t.Context(), db, j.Parents.Query().With(j.ParentAggregates.Count, j.ParentAggregates.Total), parents)
			if err != nil || g.statements != before+2 {
				t.Fatal("explicit refresh", err, g.statements-before)
			}
		})
	}
}

func TestRelationAggregateNestedAndSharedKeys(t *testing.T) {
	g := &goldenCapture{reply: aggregateReply}
	db := aggregateDB(t, g)
	childCount := j.Targets.Count.Using(expr.Related(j.TargetParent, expr.Count[j.Parent]()))
	// The reverse singular aggregate needs an unfiltered cardinality count.
	g.reply = func(sql string, args []driver.NamedValue) ([]string, [][]driver.Value) {
		cols, rows := aggregateReply(sql, args)
		if strings.Contains(sql, "GROUP BY") {
			cols = make([]string, 3)
			for i := range rows {
				rows[i][1] = int64(1)
				rows[i] = append(rows[i], int64(1))
			}
		}
		return cols, rows
	}
	parents, err := orm.LoadFrom(t.Context(), db, j.Parents.Query().With(j.DirectTargets.With(childCount)), []j.Parent{{ID: 1}, {ID: 2}})
	if err != nil || g.statements != 2 {
		t.Fatal("nested batching", err, g.statements)
	}
	for _, p := range parents {
		for _, c := range p.Targets.All() {
			v, ok := c.Count.Get()
			if !ok || v != 1 {
				t.Fatal("nested count", v, ok)
			}
		}
	}
	g.statements = 0
	g.reply = aggregateReply
	loaded, err := orm.LoadFrom(t.Context(), db, j.Parents.Query().With(j.ParentAggregates.Peers, j.ParentAggregates.Pivots), []j.Parent{{ID: 1}, {ID: 1}, {ID: 3}})
	if err != nil || g.statements != 2 {
		t.Fatal("pivot per-slot count", err, g.statements)
	}
	for i, p := range loaded {
		v, ok := p.Count.Get()
		w, wok := p.OtherCount.Get()
		if !ok || !wok || v != w || i < 2 && v != 2 || i == 2 && v != 0 {
			t.Fatal("shared/empty keys", v, w)
		}
	}
	g.statements = 0
	_, err = orm.LoadFrom(t.Context(), db, j.Parents.Query().With(j.ParentAggregates.Count), []j.Parent{})
	if err != nil || g.statements != 0 {
		t.Fatal("empty parents", err)
	}
	nullCount := j.Pivots.Count.Using(expr.Related(j.PivotParent, expr.Count[j.Parent]()))
	loadedPivot, err := orm.LoadFrom(t.Context(), db, j.Pivots.Query().With(nullCount), []j.Pivot{{ParentID: value.Null[int64]()}})
	if err != nil || g.statements != 0 {
		t.Fatal("NULL key", err)
	}
	if n, ok := loadedPivot[0].Count.Get(); !ok || n != 0 {
		t.Fatal("NULL key must attach zero")
	}
}

func TestRelationAggregateValidationBeforeSQL(t *testing.T) {
	g := &goldenCapture{reply: aggregateReply}
	db := aggregateDB(t, g)
	bad := []orm.Relation[j.Parent]{
		j.Parents.Count.Using(expr.Related(j.Chosen.LatestOfMany(), expr.Count[j.Target]())),
		j.Parents.Count.Using(expr.Related(j.DirectTargets.With(j.TargetParent), expr.Count[j.Target]())),
		j.Parents.Count.Using(expr.Related(j.Peers.WithPivot(j.PivotParent), expr.Count[j.Target]())),
		expr.Related(j.DirectTargets, expr.Count[j.Target]()),
		orm.AggregateRelation[j.Parent, int64]{},
		j.Parents.Count.Using(expr.Related(orm.HasMany[j.Parent, j.Target]{}, expr.Count[j.Target]())),
		j.Parents.Count.Using(expr.Related(j.DirectTargets, expr.OrderedAggregate[j.Target, int64]{})),
	}
	for i, r := range bad {
		if out, err := orm.LoadFrom(t.Context(), db, j.Parents.Query().With(r), []j.Parent{{ID: 1}}); out != nil || !errors.Is(err, fault.Invalid) {
			t.Fatalf("case %d: %v", i, err)
		}
	}
	morph := j.Pivots.Count.Using(expr.Related(j.MorphParent, expr.Count[j.Parent]()))
	if _, err := orm.LoadFrom(t.Context(), db, j.Pivots.Query().With(morph), []j.Pivot{{ParentID: value.Of(int64(1)), Kind: "parent"}}); !errors.Is(err, fault.Invalid) {
		t.Fatal("MorphTo", err)
	}
	if _, err := orm.All(t.Context(), db, j.Parents.Query().With(j.ParentAggregates.Count, j.ParentAggregates.Count)); !errors.Is(err, fault.Invalid) {
		t.Fatal("duplicate slots", err)
	}
	if g.statements != 0 {
		t.Fatal("invalid plan reached SQL", g.statements)
	}
}

func TestRelationAggregateFailuresDiscardResults(t *testing.T) {
	for _, tc := range []struct {
		name  string
		rows  [][]driver.Value
		rel   orm.Relation[j.Parent]
		cause error
	}{
		{"singular", [][]driver.Value{{int64(1), int64(0), int64(2)}}, j.Parents.Count.Using(expr.Related(j.Chosen, expr.Count[j.Target]().Filter(expr.Value(orm.NumberExpr(j.Targets.Rank)).Eq(-1)))), database.TooManyRows},
		{"ambiguous_pivot", [][]driver.Value{{int64(1), int64(0), int64(2), int64(1)}}, j.Parents.Count.Using(expr.Related(j.Peers, expr.Count[j.Target]().Filter(expr.Value(orm.NumberExpr(j.Targets.Rank)).Eq(-1)))), database.TooManyRows},
		{"foreign_key", [][]driver.Value{{int64(999), int64(1)}}, j.ParentAggregates.Count, fault.Invalid},
		{"duplicate_key", [][]driver.Value{{int64(1), int64(1)}, {int64(1), int64(1)}}, j.ParentAggregates.Count, fault.Invalid},
		{"decode", [][]driver.Value{{int64(1), "bad"}}, j.ParentAggregates.Count, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := &goldenCapture{reply: func(string, []driver.NamedValue) ([]string, [][]driver.Value) {
				return make([]string, len(tc.rows[0])), tc.rows
			}}
			db := aggregateDB(t, g)
			input := []j.Parent{{ID: 1}}
			out, err := orm.LoadFrom(t.Context(), db, j.Parents.Query().With(tc.rel), input)
			if out != nil || err == nil || tc.cause != nil && !errors.Is(err, tc.cause) || input[0].Count.IsLoaded() {
				t.Fatal("failure publication", err)
			}
		})
	}
	for _, limit := range []int{1, 2} {
		t.Run(fmt.Sprint("budget_", limit), func(t *testing.T) {
			g := &goldenCapture{reply: aggregateReply}
			db := aggregateDB(t, g)
			out, err := orm.All(t.Context(), db, orm.WithRelationLimits(j.Parents.Query().With(j.ParentAggregates.Count), orm.RelationLimits{BatchSize: 500, MaxRows: limit, MaxDepth: 2}))
			if out != nil || !errors.Is(err, fault.Invalid) {
				t.Fatal("group/attachment budget", err)
			}
		})
	}
	t.Run("cancelled", func(t *testing.T) {
		g := &goldenCapture{reply: aggregateReply}
		db := aggregateDB(t, g)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if out, err := orm.LoadFrom(ctx, db, j.Parents.Query().With(j.ParentAggregates.Count), []j.Parent{{ID: 1}}); out != nil || !errors.Is(err, context.Canceled) || g.statements != 0 {
			t.Fatal("cancellation", err)
		}
	})
}

func TestRelationAggregateThousandParentsAndChunkBudgets(t *testing.T) {
	for _, chunked := range []bool{false, true} {
		t.Run(fmt.Sprint(chunked), func(t *testing.T) {
			g := &goldenCapture{}
			last := int64(0)
			g.reply = func(sql string, args []driver.NamedValue) ([]string, [][]driver.Value) {
				rows := make([][]driver.Value, 0)
				if strings.Contains(sql, "GROUP BY") {
					for _, key := range aggregateBatchKeys(args) {
						rows = append(rows, []driver.Value{key, int64(1)})
					}
					return make([]string, 2), rows
				}
				n := int64(1000)
				if chunked {
					n = 200
				}
				for i := last + 1; i <= last+n; i++ {
					rows = append(rows, []driver.Value{i, "parent", nil})
				}
				last += n
				return make([]string, 3), rows
			}
			db := aggregateDB(t, g)
			maxRows := 1000
			if chunked {
				maxRows = 200
			}
			q := orm.WithRelationLimits(j.Parents.Query().Limit(1000).With(j.ParentAggregates.Count), orm.RelationLimits{BatchSize: 500, MaxRows: maxRows, MaxDepth: 2})
			seen := 0
			read := func(parents []j.Parent) error {
				for _, p := range parents {
					if n, ok := p.Count.Get(); !ok || n != 1 {
						t.Fatal("unloaded count")
					}
					seen++
				}
				return nil
			}
			if chunked {
				if err := orm.Chunk(t.Context(), db, q, 200, read); err != nil {
					t.Fatal(err, g.output.String())
				}
			} else {
				out, err := orm.All(t.Context(), db, q)
				if err != nil {
					t.Fatal(err, g.output.String())
				}
				if err := read(out); err != nil {
					t.Fatal(err, g.output.String())
				}
			}
			want := 3
			if chunked {
				want = 10
			}
			if seen != 1000 || g.statements != want {
				t.Fatal("bounded statement count", seen, g.statements, want)
			}
		})
	}
}
