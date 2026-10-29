package goway_test

import (
	"context"
	"testing"

	j "foundry.test/goway/models/joined"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

func TestComputedRelationOrderSQLMatchesOldAPI(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"direct", "target", "pivot", "mixed"} {
		t.Run(mode, func(t *testing.T) {
			captures := []*goldenCapture{{reply: joinedReply}}
			for _, g := range captures {
				db := g.open(ctx)

				if mode == "direct" {
					r := j.TargetParent
					r = r.OrderBy(expr.RelationOrder(r, expr.Lower(expr.Value(orm.TextExpr(j.Parents.Name))).Asc()))
					if _, err := orm.LoadFrom(ctx, db, j.Targets.Query().With(r), []j.Target{{ID: 10, ParentID: 1}}); err != nil {
						t.Fatal(err)
					}
				} else {
					r := j.Peers
					target := expr.RelationOrder(r, expr.Add(expr.Value(orm.NumberExpr(j.Targets.Rank)), expr.Param(orm.NumberExpr(j.Targets.Rank), 2)).Desc())
					pivot := expr.RelationPivotOrder(r, expr.Lower(expr.Value(orm.TextExpr(j.Pivots.Role))).Asc())
					switch mode {
					case "target":
						r = r.OrderBy(target)
					case "pivot":
						r = r.OrderByPivot(pivot)
					case "mixed":
						r = r.OrderByPivot(pivot).OrderBy(target)
					}
					if _, err := orm.LoadFrom(ctx, db, j.Parents.Query().With(r), []j.Parent{{ID: 1}}); err != nil {
						t.Fatal(err)
					}
				}

				if err := db.Close(ctx); err != nil {
					t.Fatal(err)
				}
				if g.statements != 1 {
					t.Fatal("unexpected query count", g.statements)
				}
			}
			// The shared oracle records legacy SQL and bindings for each mode, then
			// checks the current transcript against those records during replay.
			if !oracleEqual(t, "goway/relation_order_golden/1", captures[0].output.String()) {
				t.Fatal("frozen legacy comparison differs")
			}
		})
	}
}
