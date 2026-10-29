package goway_test

import (
	"context"
	"database/sql/driver"
	"fmt"
	"testing"
	"time"

	"foundry.test/goway/coremodels"
	"foundry.test/goway/models"
	n "foundry.test/goway/models/hooks"

	"github.com/weiloon1234/Foundry-Go/database"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"

	"github.com/weiloon1234/Foundry-Go/model"
)

func TestStoreUUIDSeekGolden(t *testing.T) {
	id := must(model.ParseID[models.Order](orderID))

	newPred := []orm.Predicate[models.Order]{orm.UUIDLess(orm.KeyExpr(models.Orders.ID), id), orm.UUIDLessOrEqual(orm.KeyExpr(models.Orders.ID), id), orm.UUIDGreater(orm.KeyExpr(models.Orders.ID), id), orm.UUIDGreaterOrEqual(orm.KeyExpr(models.Orders.ID), id)}
	newExpr := []expr.Predicate[models.Order]{expr.UUIDLess(expr.Value(orm.KeyExpr(models.Orders.ID)), id), expr.UUIDLessOrEqual(expr.Value(orm.KeyExpr(models.Orders.ID)), id), expr.UUIDGreater(expr.Value(orm.KeyExpr(models.Orders.ID)), id), expr.UUIDGreaterOrEqual(expr.Value(orm.KeyExpr(models.Orders.ID)), id)}

	for i := range newPred {
		for _, computed := range []bool{false, true} {
			g := &goldenCapture{scriptedColumns: []string{"id", "buyer_id", "total_cents"}, scriptedBatches: [][][]driver.Value{{{orderID, buyerID, int64(12)}}}}
			db := g.open(t.Context())
			defer db.Close(t.Context())
			q := models.Orders.Query()
			if computed {
				q = expr.Where(q, newExpr[i])
			} else {
				q = q.Where(newPred[i])
			}
			got := must(orm.All(t.Context(), db, q.OrderBy(orm.KeyAsc(models.Orders.ID)).Limit(3)))
			fmt.Fprintln(&g.output, oracleSnapshot([]any{got}))
			if !oracleEqual(t, "store_capabilities_golden/1", g.output.String()) {
				t.Fatal("frozen UUID seek differs")
			}
		}
	}
}

func TestStoreEachLifecycleGolden(t *testing.T) {
	for _, remove := range []bool{false, true} {
		var transcripts []string
		{
			now := time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC)
			row := func(id int64, name string, deleted driver.Value) []driver.Value {
				return []driver.Value{id, name, nil, []byte{1, 2}, now, now, deleted}
			}
			deleted := driver.Value(nil)
			if remove {
				deleted = now
			}
			g := &goldenCapture{scriptedColumns: []string{"id", "name", "note", "body", "created_at", "updated_at", "deleted_at"}, scriptedBatches: [][][]driver.Value{
				{row(1, "one", nil), row(2, "two", nil)}, {row(1, "one", nil)}, {row(1, "changed", deleted)}, {row(2, "two", nil)}, {row(2, "changed", deleted)},
			}}
			var trace []string
			ctx := coremodels.LifecycleContext(t.Context(), &trace)
			db := g.open(ctx)
			defer db.Close(ctx)

			q := n.Members.Query().Where(orm.InputTextNe(n.Members.Name, "omit"))
			if remove {
				v, err := orm.DeleteEach(ctx, db, q, 2)
				if err != nil {
					t.Fatal(err)
				}
				fmt.Fprintln(&g.output, oracleSnapshot([]any{v}))
			} else {
				v, err := orm.UpdateEach(ctx, db, q, 2, func(ctx context.Context, _ *database.Tx, m n.Member) ([]orm.Assignment[n.Member], error) {
					coremodels.TraceLifecycle(ctx, "draft")
					return []orm.Assignment[n.Member]{orm.InputTextSet(n.Members.Name, " changed ")}, nil
				})
				if err != nil {
					t.Fatal(err)
				}
				fmt.Fprintln(&g.output, oracleSnapshot([]any{v}))
			}

			fmt.Fprintln(&g.output, oracleSnapshot([]any{trace}))
			if g.statements != 5 {
				t.Fatal(g.statements)
			}
			transcripts = append(transcripts, g.output.String())
		}
		if !oracleEqual(t, "store_capabilities_golden/2", transcripts[0]) {
			t.Fatal("frozen legacy comparison differs")
		}
	}
}
