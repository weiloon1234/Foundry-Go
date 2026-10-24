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
	o "foundry.test/goway/oldmodels/hooks"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/model"
)

func TestStoreUUIDSeekGolden(t *testing.T) {
	id := must(model.ParseID[models.Order](orderID))
	f := query.NewOrderedField[models.Order, model.ID[models.Order]]("orders", "id", codec.ID[models.Order]())
	old := query.ForModel(query.Define("orders", "id", []query.Column{{Name: "id"}, {Name: "buyer_id"}, {Name: "total_cents"}}, func(row database.Row) (models.Order, error) {
		var m models.Order
		err := row.Scan(codec.ID[models.Order]().Scan(&m.ID), codec.ID[models.User]().Scan(&m.BuyerID), &m.TotalCents)
		return m, err
	}))
	oldPred := []query.Predicate[models.Order]{f.Lt(id), f.Lte(id), f.Gt(id), f.Gte(id)}
	newPred := []orm.Predicate[models.Order]{orm.UUIDLess(models.Orders.ID, id), orm.UUIDLessOrEqual(models.Orders.ID, id), orm.UUIDGreater(models.Orders.ID, id), orm.UUIDGreaterOrEqual(models.Orders.ID, id)}
	newExpr := []expr.Predicate[models.Order]{expr.UUIDLess(expr.Value(models.Orders.ID), id), expr.UUIDLessOrEqual(expr.Value(models.Orders.ID), id), expr.UUIDGreater(expr.Value(models.Orders.ID), id), expr.UUIDGreaterOrEqual(expr.Value(models.Orders.ID), id)}
	for i := range oldPred {
		for _, computed := range []bool{false, true} {
			before, after := &goldenCapture{}, &goldenCapture{}
			for _, g := range []*goldenCapture{before, after} {
				g.scriptedColumns = []string{"id", "buyer_id", "total_cents"}
				g.scriptedBatches = [][][]driver.Value{{{orderID, buyerID, int64(12)}}}
			}
			odb, ndb := before.open(t.Context()), after.open(t.Context())
			defer odb.Close(t.Context())
			defer ndb.Close(t.Context())
			a := must(old.Where(oldPred[i]).OrderBy(f.Asc()).Limit(3).All(t.Context(), odb))
			q := models.Orders.Query()
			if computed {
				q = expr.Where(q, newExpr[i])
			} else {
				q = q.Where(newPred[i])
			}
			b := must(q.OrderBy(models.Orders.ID.Asc()).Limit(3).All(t.Context(), ndb))
			fmt.Fprintln(&before.output, a)
			fmt.Fprintln(&after.output, b)
			if before.output.String() != after.output.String() {
				t.Fatal(goldenDiff(before.output.String(), after.output.String()))
			}
		}
	}
}

func TestStoreEachLifecycleGolden(t *testing.T) {
	for _, remove := range []bool{false, true} {
		var transcripts []string
		for _, old := range []bool{true, false} {
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
			if old {
				q := o.QueryHookMembers().Where(o.MemberFields().Name.Ne("omit"))
				if remove {
					v, err := q.DeleteEach(ctx, db, 2)
					if err != nil {
						t.Fatal(err)
					}
					fmt.Fprintln(&g.output, v)
				} else {
					v, err := q.UpdateEach(ctx, db, 2, func(ctx context.Context, _ *database.Tx, m o.Member) (o.MemberDraft, error) {
						coremodels.TraceLifecycle(ctx, "draft")
						return o.MemberDraft{}.SetName(" changed "), nil
					})
					if err != nil {
						t.Fatal(err)
					}
					fmt.Fprintln(&g.output, v)
				}
			} else {
				q := n.Members.Query().Where(n.Members.Name.Ne("omit"))
				if remove {
					v, err := orm.DeleteEach(ctx, db, q, 2)
					if err != nil {
						t.Fatal(err)
					}
					fmt.Fprintln(&g.output, v)
				} else {
					v, err := orm.UpdateEach(ctx, db, q, 2, func(ctx context.Context, _ *database.Tx, m n.Member) ([]orm.Assignment[n.Member], error) {
						coremodels.TraceLifecycle(ctx, "draft")
						return []orm.Assignment[n.Member]{n.Members.Name.Set(" changed ")}, nil
					})
					if err != nil {
						t.Fatal(err)
					}
					fmt.Fprintln(&g.output, v)
				}
			}
			fmt.Fprintln(&g.output, trace)
			if g.statements != 5 {
				t.Fatal(g.statements)
			}
			transcripts = append(transcripts, g.output.String())
		}
		if transcripts[0] != transcripts[1] {
			t.Fatal(goldenDiff(transcripts[0], transcripts[1]))
		}
	}
}
