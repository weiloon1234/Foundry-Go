package goway_test

import (
	"database/sql/driver"
	j "foundry.test/goway/models/joined"
	old "foundry.test/goway/oldmodels/joined"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"strings"
	"testing"
)

// Deliberately borrow the target marker while retaining a different target's plan.
type filterTargetMarker struct {
	orm.BelongsTo[j.Parent, j.Parent]
}
type borrowedFilterTarget struct {
	orm.Relation[j.Parent]
	filterTargetMarker
}

func TestRelationExpressionFilterSQLParity(t *testing.T) {
	for _, kind := range []string{"belongs", "one", "many", "through", "nested-correlation"} {
		t.Run(kind, func(t *testing.T) {
			// Dispatch by the outer SELECT shape. Tables inside WHERE/EXISTS do
			// not determine the columns returned by the outer query.
			reply := func(sql string, args []driver.NamedValue) ([]string, [][]driver.Value) {
				head, _, _ := strings.Cut(sql, " WHERE ")
				return joinedReply(head, args)
			}
			captures := []*goldenCapture{{reply: reply}, {reply: reply}}
			for side, g := range captures {
				db := g.open(t.Context())
				if side == 0 {
					p := old.TargetFields().Rank.GtColumn(old.TargetFields().ParentID)
					if kind == "nested-correlation" {
						link := query.Correlate(old.QueryJoinedTargets(), old.QueryJoinedParents())
						outer := old.TargetFieldsAt(query.OuterScope(link, old.QueryJoinedTargets().Scope()))
						inner := old.ParentFieldsAt(query.InnerScope(link, old.QueryJoinedParents().Scope()))
						p = link.Where(inner.ID.EqColumn(outer.ParentID)).Exists()
					}
					var err error
					switch kind {
					case "belongs":
						_, err = old.QueryJoinedTargets().With(old.TargetRelations().Parent.Where(old.ParentFields().Name.Contains("one"))).All(t.Context(), db)
					case "one":
						_, err = old.QueryJoinedParents().With(old.ParentRelations().Chosen.Where(p)).All(t.Context(), db)
					case "many":
						_, err = old.QueryJoinedParents().With(old.ParentRelations().Targets.Where(p)).All(t.Context(), db)
					default:
						r := old.ParentRelations().Peers.Where(p)
						_, err = old.QueryJoinedParents().WhereHas(r).With(r).All(t.Context(), db)
					}
					if err != nil {
						t.Fatal(err)
					}
				} else {
					p := expr.GreaterColumn(j.Targets.Rank, j.Targets.ParentID)
					if kind == "nested-correlation" {
						link := rowset.Correlate(rowset.From(j.Targets.Query()), rowset.From(j.Parents.Query()))
						outer := rowset.OuterScope(link, rowset.ModelScope(j.Targets.Query()))
						inner := rowset.InnerScope(link, rowset.ModelScope(j.Parents.Query()))
						p = link.Where(expr.Equal(expr.At(inner, j.Parents.ID), expr.At(outer, j.Targets.ParentID))).Exists()
					}
					var err error
					switch kind {
					case "belongs":
						r := j.TargetParent
						_, err = j.Targets.Query().With(r.Where(expr.RelationFilter(r, expr.TextRow(j.Parents.Name).Contains("one")))).All(t.Context(), db)
					case "one":
						r := j.Chosen
						_, err = j.Parents.Query().With(r.Where(expr.RelationFilter(r, p))).All(t.Context(), db)
					case "many":
						r := j.TargetsThrough
						_, err = j.Parents.Query().With(r.Where(expr.RelationFilter(r, p))).All(t.Context(), db)
					default:
						r := j.Peers.Where(expr.RelationFilter(j.Peers, p))
						_, err = j.Parents.Query().Where(orm.Has(r)).With(r).All(t.Context(), db)
					}
					if err != nil {
						t.Fatal(err)
					}
				}
				if err := db.Close(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			if !oracleEqual(t, "relation_filter/1", captures[0].output.String(), captures[1].output.String()) {
				t.Fatal(goldenDiff(captures[0].output.String(), captures[1].output.String()))
			}
		})
	}
}
