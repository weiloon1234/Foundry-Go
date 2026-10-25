package goway_test

import (
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	j "foundry.test/goway/models/joined"
	old "foundry.test/goway/oldmodels/joined"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// Deliberately borrow the target marker while retaining a different target's plan.
type filterTargetMarker struct {
	orm.BelongsTo[j.Parent, j.Parent]
}
type borrowedFilterTarget struct {
	orm.Relation[j.Parent]
	filterTargetMarker
}

func TestRelationFilterRejectsForeignMetadata(t *testing.T) {
	foreign := orm.Define("joined_targets", func(b orm.Builder[j.Target], m *j.Target) j.TargetFields {
		table := orm.TableOf(b, &m.ID)
		return j.TargetFields{Table: table, ID: table.Key(), ParentID: orm.NumberOf(b, &m.ParentID), Name: orm.TextOf(b, &m.Name), Rank: orm.NumberOf(b, &m.Rank), DeletedAt: orm.SoftDeleteOf(b, &m.DeletedAt), Parent: orm.OneOf(b, &m.Parent), Inverse: orm.ThroughOf(b, &m.Inverse)}
	})
	for name, p := range map[string]expr.Predicate[j.Target]{
		"foreign":       expr.TextRow(foreign.Name).Contains("a"),
		"zero":          {},
		"foreign-scope": expr.TextRowAt(rowset.ModelScope(foreign.Query()), foreign.Name).Contains("a"),
	} {
		t.Run(name, func(t *testing.T) {
			r := j.Peers.Where(expr.RelationFilter(j.Peers, p))
			if err := orm.Check(r); !errors.Is(err, fault.Invalid) {
				t.Fatal("accepted invalid filter", err)
			}
			g := &goldenCapture{reply: joinedReply}
			db := g.open(t.Context())
			defer db.Close(t.Context())
			if _, err := j.Parents.Query().With(r).All(t.Context(), db); !errors.Is(err, fault.Invalid) || g.statements != 0 {
				t.Fatal("invalid filter reached SQL", err, g.statements)
			}
		})
	}
	for name, invalidRelation := range map[string]interface {
		orm.ExistenceRelation[j.Target, j.Parent]
		orm.CheckedTable
	}{
		"direct":  orm.BelongsToOf(j.Targets.Parent, foreign.ParentID, j.Parents.ID),
		"through": orm.ManyToManyOf(j.Targets.Inverse, foreign.ID, j.Pivots.TargetID, j.Pivots.ParentID, j.Parents.ID),
	} {
		t.Run("construction/"+name, func(t *testing.T) {
			if err := orm.Check(invalidRelation); !errors.Is(err, fault.Invalid) {
				t.Fatal("expected malformed relation", err)
			}
			p := expr.RelationFilter(invalidRelation, expr.TextRow(j.Parents.Name).Contains("one"))
			if _, err := rowset.Compile(rowset.FromModel(j.Parents.Query().Where(p))); !errors.Is(err, fault.Invalid) {
				t.Fatal("target query lost construction error", err)
			}
			validRelation := j.TargetParent.Where(p)
			if err := orm.Check(validRelation); !errors.Is(err, fault.Invalid) {
				t.Fatal("valid relation lost transferred construction error", err)
			}
			g := &goldenCapture{reply: joinedReply}
			db := g.open(t.Context())
			defer db.Close(t.Context())
			if _, err := j.Parents.Query().Where(p).All(t.Context(), db); !errors.Is(err, fault.Invalid) {
				t.Fatal("target query executed invalid adapted predicate", err)
			}
			if _, err := j.Targets.Query().With(validRelation).All(t.Context(), db); !errors.Is(err, fault.Invalid) {
				t.Fatal("valid relation executed invalid adapted predicate", err)
			}
			for name, predicate := range map[string]orm.Predicate[j.Target]{
				"Has":        orm.Has(invalidRelation),
				"DoesntHave": orm.DoesntHave(invalidRelation),
			} {
				if _, err := rowset.Compile(rowset.FromModel(j.Targets.Query().Where(predicate))); !errors.Is(err, fault.Invalid) {
					t.Fatal(name+" lost construction error", err)
				}
			}
			if _, err := rowset.Compile(rowset.FromModel(j.Targets.Query()).Where(expr.Has(invalidRelation, expr.TextRow(j.Parents.Name).Contains("one")))); !errors.Is(err, fault.Invalid) {
				t.Fatal("expr.Has lost construction error", err)
			}
			if g.statements != 0 {
				t.Fatal("invalid adapted predicate reached SQL", g.statements)
			}
		})
	}

	fake := borrowedFilterTarget{Relation: j.Peers}
	rejected := expr.RelationFilter(fake, expr.TextRow(j.Parents.Name).Contains("x"))
	if _, err := rowset.Compile(rowset.FromModel(j.Parents.Query().Where(rejected))); !errors.Is(err, fault.Invalid) {
		t.Fatal("borrowed target accepted", err)
	}
	var nilRelation *orm.ThroughRelation[j.Parent, j.Target, j.Pivot]
	for _, r := range []orm.ExistenceRelation[j.Parent, j.Target]{nil, nilRelation, orm.HasMany[j.Parent, j.Target]{}} {
		p := expr.RelationFilter(r, expr.TextRow(j.Targets.Name).Contains("x"))
		if _, err := rowset.Compile(rowset.FromModel(j.Targets.Query().Where(p))); !errors.Is(err, fault.Invalid) {
			t.Fatal("nil/zero relation accepted", err)
		}
	}
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
