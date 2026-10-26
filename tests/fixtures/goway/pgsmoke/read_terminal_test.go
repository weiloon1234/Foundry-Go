package pgsmoke_test

import (
	"context"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func TestStage9ReadTerminalsReleaseSessions(t *testing.T) {
	db := postgres.Isolate(t).Open(t)
	ctx := t.Context()
	_, err := db.Exec(ctx, `CREATE TABLE iteration_nodes (id bigint PRIMARY KEY,parent_id bigint NOT NULL)`)
	must(t, err)
	_, err = db.Exec(ctx, `INSERT INTO iteration_nodes SELECT n,0 FROM generate_series(1,231) n`)
	must(t, err)
	factories, retrieved := 0, 0
	table := orm.Define("iteration_nodes", func(b orm.Builder[treeNode], n *treeNode) treeTable {
		return treeTable{orm.TableOf(b, &n.ID), orm.NumberOf(b, &n.ParentID)}
	}, orm.WithRetrievalHooks(func() orm.RetrievalHooks[treeNode] {
		factories++
		return orm.RetrievalHooks[treeNode]{Retrieved: func(ctx context.Context, ex database.Executor, _ treeNode) error {
			retrieved++
			_, err := ex.Exec(ctx, "SELECT 1")
			return err
		}}
	}))
	a := rows.AsRecord[joinLeft](rows.CTE("eligible", rows.ModelRecord(table.Query())), "n")
	q := rows.SelectRecord(a, rows.Scope(a)).OrderBy(expr.At(rows.Scope(a), table.Key()).Asc()).Offset(3).Limit(205)
	must(t, rows.PreflightRead(ctx, q))
	if _, err := rows.CompileRead(q); err != nil {
		t.Fatal(err)
	}
	seen := 0
	must(t, db.Session(ctx, func(s *database.Session) error {
		if err := rows.EachRead(ctx, s, q, func(n treeNode) error {
			seen++
			if n.ID != int64(seen+3) {
				t.Fatal(n.ID, seen)
			}
			return nil
		}); err != nil {
			return err
		}
		if factories != 3 || retrieved != 205 || seen != 205 {
			t.Fatal(factories, retrieved, seen)
		}
		stop := errors.New("stop record")
		if err := rows.EachRead(ctx, s, q, func(treeNode) error { return stop }); !errors.Is(err, stop) {
			t.Fatal(err)
		}
		scalar := rows.ReadScalar(rows.SelectScalar(a, expr.Row(expr.At(rows.Scope(a), table.Key())).Value()))
		if err := rows.EachRead(ctx, s, scalar, func(int64) error { return stop }); !errors.Is(err, stop) {
			t.Fatal(err)
		}
		combined := rows.ReadSet(rows.Union(rows.ModelRecord(table.Query()), rows.ModelRecord(table.Query())))
		if err := rows.EachRead(ctx, s, combined, func(treeNode) error { return stop }); !errors.Is(err, stop) {
			t.Fatal(err)
		}
		_, err := s.Exec(ctx, "SELECT 1")
		return err
	}))
	if db.Stats().InUse != 0 || db.Stats().Owners != 0 {
		t.Fatal("stream retained owner", db.Stats())
	}
}
