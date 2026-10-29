package goway_test

import "github.com/weiloon1234/Foundry-Go/model"

import (
	"context"
	"database/sql/driver"
	"errors"
	j "foundry.test/goway/models/joined"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/testkit/dbassert"
	"github.com/weiloon1234/Foundry-Go/value"
	"strconv"
	"strings"
	"testing"
)

func TestJoinedBatchTreeCountsAndPivotOwnership(t *testing.T) {
	d, err := database.NewStatementDetector(database.StatementDetectorConfig{Threshold: 2, MaxShapes: 16})
	if err != nil {
		t.Fatal(err)
	}
	g := &goldenCapture{detector: d}
	closed := 0
	g.onClose = func() { closed++ }
	g.reply = func(sql string, args []driver.NamedValue) ([]string, [][]driver.Value) {
		if closed != g.statements-1 {
			t.Fatal("next statement before rows closed")
		}
		if !strings.Contains(sql, `AS "foundry_pivot"`) {
			if strings.Contains(sql, `FROM "joined_parents"`) && !strings.Contains(sql, " LIMIT ") {
				return []string{"id", "name", "deleted_at"}, [][]driver.Value{{int64(1), "one", nil}, {int64(2), "two", nil}}
			}
			return joinedReply(sql, args)
		}
		keys := map[int64]bool{}
		for _, a := range args {
			if text, ok := a.Value.(string); ok && strings.HasPrefix(text, "{") {
				for _, word := range strings.Split(strings.Trim(text, "{}"), ",") {
					n, _ := strconv.ParseInt(word, 10, 64)
					keys[n] = true
				}
			}
		}
		var rows [][]driver.Value
		for _, edge := range []struct {
			id, owner int64
			role      string
		}{{100, 1, "a"}, {101, 2, "b"}, {102, 1, "c"}} {
			if keys[edge.owner] {
				rows = append(rows, []driver.Value{int64(10), int64(1), "ten", int64(7), nil, edge.id, edge.owner, int64(10), "parent", edge.role, nil})
			}
		}
		return make([]string, 11), rows
	}
	ctx, scope := d.Begin(t.Context())
	db := g.open(ctx)
	t.Cleanup(func() {
		if err := db.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	r := j.Peers.With(j.TargetParent).WithPivot(j.PivotTarget)
	q := orm.WithRelationLimits(j.Parents.Query().With(r), orm.RelationLimits{BatchSize: 1, MaxRows: 100, MaxDepth: 3})
	// Load uses defaults, so use a query to exercise the custom batch size, then
	// inspect the same tree using explicit parent loading without a parent read.
	parents, err := orm.All(ctx, db, q.Where(orm.KeyIn(j.Parents.ID, 1, 2)))
	if err != nil {
		t.Fatal(err)
	}
	// Two key batches must finish before either nested branch loads its complete collection.
	if len(parents) != 2 || g.statements != 5 {
		t.Fatal("query tree count", len(parents), g.statements)
	}
	dbassert.AssertStatementShapes(t, scope)
	ctx, scope = d.Begin(t.Context())
	before := g.statements
	loaded, err := orm.LoadFrom(ctx, db, j.Parents.Query().With(r), []j.Parent{{ID: 1}, {ID: 2}, {ID: 3}})
	if err != nil {
		t.Fatal(err)
	}
	if g.statements-before != 3 || closed != g.statements {
		t.Fatal("joined plus target and pivot branches", g.statements-before, closed)
	}
	if loaded[0].Peers.Len() != 2 || loaded[1].Peers.Len() != 1 || !loaded[2].Peers.IsLoaded() || loaded[2].Peers.Len() != 0 {
		t.Fatal("distinct pivots or empty links lost")
	}
	for i, p := range loaded {
		for _, link := range p.Peers.All() {
			owner, _ := link.Pivot.ParentID.Get()
			if owner != int64(i+1) || link.Model.ID != 10 || !link.Model.Parent.IsLoaded() || !link.Pivot.Target.IsLoaded() {
				t.Fatal("cross-parent pivot or missing nested branch")
			}
		}
	}
	before = g.statements
	_, err = orm.LoadMissingFrom(ctx, db, j.Parents.Query().With(r), loaded)
	if err != nil || g.statements != before {
		t.Fatal("loaded-empty reloaded", err)
	}
	dbassert.AssertStatementShapes(t, scope)
}

func TestJoinedFailuresPublishNoPartialParents(t *testing.T) {
	for _, mode := range []string{"duplicate_pivot", "mismatched_key", "decode", "budget", "duplicate_order", "depth", "of_many_empty"} {
		t.Run(mode, func(t *testing.T) {
			g := &goldenCapture{reply: joinedReply}
			closed := 0
			g.onClose = func() { closed++ }
			ctx := t.Context()
			db := g.open(ctx)
			t.Cleanup(func() { _ = db.Close(context.Background()) })
			r := j.Peers
			want := error(fault.Invalid)
			switch mode {
			case "duplicate_pivot":
				want = database.TooManyRows
				g.reply = func(s string, a []driver.NamedValue) ([]string, [][]driver.Value) {
					c, rows := joinedReply(s, a)
					return c, append(rows, rows[0])
				}
			case "mismatched_key":
				g.reply = func(s string, a []driver.NamedValue) ([]string, [][]driver.Value) {
					c, rows := joinedReply(s, a)
					rows[0][7] = int64(99)
					return c, rows
				}
			case "decode":
				g.reply = func(s string, a []driver.NamedValue) ([]string, [][]driver.Value) {
					c, rows := joinedReply(s, a)
					rows[0][0] = "invalid integer"
					return c, rows
				}
				want = nil
			case "duplicate_order":
				r = r.OrderByPivot(orm.TextAsc(j.Pivots.Role), orm.TextDesc(j.Pivots.Role))
			}
			if mode == "of_many_empty" {
				_, err := orm.LoadFrom(ctx, db, j.Parents.Query().With(j.Chosen.OfMany()), []j.Parent{{ID: 1}})
				if !errors.Is(err, fault.Invalid) || g.statements != 0 {
					t.Fatal(err, g.statements)
				}
				return
			}
			var err error
			var result []j.Parent
			if mode == "budget" || mode == "depth" {
				limits := orm.RelationLimits{BatchSize: 10, MaxRows: 1, MaxDepth: 3}
				if mode == "depth" {
					limits.MaxRows = 100
					limits.MaxDepth = 1
					r = r.With(j.TargetParent)
				}
				result, err = orm.All(ctx, db, orm.WithRelationLimits(j.Parents.Query().With(r), limits))
			} else {
				result, err = orm.LoadFrom(ctx, db, j.Parents.Query().With(r), []j.Parent{{ID: 1}})
			}
			if result != nil || err == nil || want != nil && !errors.Is(err, want) {
				t.Fatal("partial or wrong error", result, err)
			}
			if mode == "duplicate_order" || mode == "depth" {
				if g.statements != 0 {
					t.Fatal("invalid graph sent SQL")
				}
			}
			if closed != g.statements {
				t.Fatal("rows left open")
			}
		})
	}
}
func TestMorphEligibilityHasNoEmptyBatchSQL(t *testing.T) {
	g := &goldenCapture{reply: joinedReply}
	ctx := t.Context()
	db := g.open(ctx)
	t.Cleanup(func() { _ = db.Close(context.Background()) })
	rows, err := orm.LoadFrom(ctx, db, j.Pivots.Query().With(j.MorphParent, j.MorphTarget), []j.Pivot{{ID: 1, ParentID: value.Of(int64(1)), Kind: "other"}, {ID: 2, Kind: "parent"}})
	if err != nil || g.statements != 0 {
		t.Fatal(err, g.statements)
	}
	for _, row := range rows {
		a, loaded := row.MorphParent.Get()
		b, other := row.MorphTarget.Get()
		if !loaded || !other || a.IsSet() || b.IsSet() {
			t.Fatal("ineligible slot not loaded empty")
		}
	}
}

type invalidJoinedMorph struct {
	ID       int64
	Children relation.Many[j.Pivot]
}

func (invalidJoinedMorph) MorphName() model.MorphName { return "invalid name" }
func TestInvalidMorphDiscriminatorFailsBeforeSQL(t *testing.T) {
	type fields struct {
		orm.Table[invalidJoinedMorph, int64]
		Children orm.Many[invalidJoinedMorph, j.Pivot]
	}
	table := orm.Define("invalid_morph", func(b orm.Builder[invalidJoinedMorph], m *invalidJoinedMorph) fields {
		return fields{orm.TableOf(b, &m.ID), orm.ManyOf(b, &m.Children)}
	})
	r := orm.MorphManyOf(table.Children, orm.KeyExpr(table.Key()), orm.NullableNumberExpr(j.Pivots.ParentID), orm.ColumnExpr(j.Pivots.Kind))
	g := &goldenCapture{reply: joinedReply}
	ctx := t.Context()
	db := g.open(ctx)
	t.Cleanup(func() { _ = db.Close(context.Background()) })
	if result, err := orm.All(ctx, db, table.Query().With(r)); result != nil || !errors.Is(err, fault.Invalid) || g.statements != 0 {
		t.Fatal("invalid morph declaration sent SQL", err, g.statements)
	}
}

func TestJoinedLockedTransactionAndEmptySets(t *testing.T) {
	g := &goldenCapture{reply: joinedReply}
	closed := 0
	g.onClose = func() { closed++ }
	ctx := t.Context()
	db := g.open(ctx)
	t.Cleanup(func() { _ = db.Close(context.Background()) })
	err := db.Transaction(ctx, func(tx *database.Tx) error {
		rows, err := orm.AllLocked(ctx, tx, orm.Lock(j.Parents.Query().Where(orm.KeyEq(j.Parents.ID, 1)).With(j.Peers)))
		if err != nil {
			return err
		}
		if len(rows) != 1 || rows[0].Peers.Len() != 1 {
			t.Fatal("locked joined result")
		}
		return nil
	})
	if err != nil || g.statements != 2 || closed != 2 || !strings.Contains(g.output.String(), "FOR UPDATE") {
		t.Fatal("locked transaction/count", err, g.statements, closed)
	}
	before := g.statements
	result, err := orm.LoadFrom(ctx, db, j.Parents.Query().With(j.Peers, j.TargetsThrough, j.MorphChildren), []j.Parent{})
	if err != nil || len(result) != 0 || g.statements != before {
		t.Fatal("empty sets sent joined SQL", err)
	}
}
