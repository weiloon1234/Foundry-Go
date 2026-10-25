package goway_test

import (
	"context"
	"testing"

	j "foundry.test/goway/models/joined"
	old "foundry.test/goway/oldmodels/joined"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func TestComputedRelationOrderSQLMatchesOldAPI(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"direct", "target", "pivot", "mixed"} {
		t.Run(mode, func(t *testing.T) {
			captures := []*goldenCapture{{reply: joinedReply}, {reply: joinedReply}}
			for side, g := range captures {
				db := g.open(ctx)
				if side == 0 {
					target := query.Add(old.TargetFields().Rank, query.Param(old.TargetFields().Rank, 2)).Desc()
					pivot := query.Lower(old.PivotFields().Role).Asc()
					if mode == "direct" {
						_, err := old.QueryJoinedTargets().With(old.TargetRelations().Parent.OrderBy(query.Lower(old.ParentFields().Name).Asc())).Load(ctx, db, []old.Target{{ID: 10, ParentID: 1}})
						if err != nil {
							t.Fatal(err)
						}
					} else {
						r := old.ParentRelations().Peers
						switch mode {
						case "target":
							r = r.OrderBy(target)
						case "pivot":
							r = r.OrderByPivot(pivot)
						case "mixed":
							r = r.OrderByPivot(pivot).OrderBy(target)
						}
						if _, err := old.QueryJoinedParents().With(r).Load(ctx, db, []old.Parent{{ID: 1}}); err != nil {
							t.Fatal(err)
						}
					}
				} else {
					if mode == "direct" {
						r := j.TargetParent
						r = r.OrderBy(expr.RelationOrder(r, expr.Lower(expr.Value(j.Parents.Name)).Asc()))
						if _, err := orm.Load(ctx, db, []j.Target{{ID: 10, ParentID: 1}}, r); err != nil {
							t.Fatal(err)
						}
					} else {
						r := j.Peers
						target := expr.RelationOrder(r, expr.Add(expr.Value(j.Targets.Rank), expr.Param(j.Targets.Rank, 2)).Desc())
						pivot := expr.RelationPivotOrder(r, expr.Lower(expr.Value(j.Pivots.Role)).Asc())
						switch mode {
						case "target":
							r = r.OrderBy(target)
						case "pivot":
							r = r.OrderByPivot(pivot)
						case "mixed":
							r = r.OrderByPivot(pivot).OrderBy(target)
						}
						if _, err := orm.Load(ctx, db, []j.Parent{{ID: 1}}, r); err != nil {
							t.Fatal(err)
						}
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
			if !oracleEqual(t, "goway/relation_order_golden/1", captures[0].output.String(), captures[1].output.String()) {
				t.Fatal(goldenDiff(captures[0].output.String(), captures[1].output.String()))
			}
		})
	}
}
