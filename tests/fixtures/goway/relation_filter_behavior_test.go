package goway_test

import (
	"errors"
	j "foundry.test/goway/models/joined"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/fault"
	"testing"
)

func TestRelationFilterRejectsForeignMetadata(t *testing.T) {
	foreign := orm.Define("joined_targets", func(b orm.Builder[j.Target], m *j.Target) j.TargetFields {
		table := orm.TableOf(b, &m.ID)
		return j.TargetFields{Table: table, ID: table.Key(), ParentID: orm.NumberOf(b, &m.ParentID), Name: orm.TextOf(b, &m.Name), Rank: orm.NumberOf(b, &m.Rank), DeletedAt: orm.SoftDeleteOf(b, &m.DeletedAt), Parent: orm.OneOf(b, &m.Parent), Inverse: orm.ThroughOf(b, &m.Inverse)}
	})
	for name, p := range map[string]expr.Predicate[j.Target]{
		"foreign":       expr.TextRow(orm.TextExpr(foreign.Name)).Contains("a"),
		"zero":          {},
		"foreign-scope": expr.TextRowAt(rowset.ModelScope(foreign.Query()), orm.TextExpr(foreign.Name)).Contains("a"),
	} {
		t.Run(name, func(t *testing.T) {
			r := j.Peers.Where(expr.RelationFilter(j.Peers, p))
			if err := orm.Check(r); !errors.Is(err, fault.Invalid) {
				t.Fatal("accepted invalid filter", err)
			}
			g := &goldenCapture{reply: joinedReply}
			db := g.open(t.Context())
			defer db.Close(t.Context())
			if _, err := orm.All(t.Context(), db, j.Parents.Query().With(r)); !errors.Is(err, fault.Invalid) || g.statements != 0 {
				t.Fatal("invalid filter reached SQL", err, g.statements)
			}
		})
	}
	for name, invalidRelation := range map[string]interface {
		orm.ExistenceRelation[j.Target, j.Parent]
		orm.CheckedTable
	}{
		"direct":  orm.BelongsToOf(j.Targets.Parent, orm.NumberExpr(foreign.ParentID), orm.KeyExpr(j.Parents.ID)),
		"through": orm.ManyToManyOf(j.Targets.Inverse, orm.KeyExpr(foreign.ID), orm.NullableNumberExpr(j.Pivots.TargetID), orm.NullableNumberExpr(j.Pivots.ParentID), orm.KeyExpr(j.Parents.ID)),
	} {
		t.Run("construction/"+name, func(t *testing.T) {
			if err := orm.Check(invalidRelation); !errors.Is(err, fault.Invalid) {
				t.Fatal("expected malformed relation", err)
			}
			p := expr.RelationFilter(invalidRelation, expr.TextRow(orm.TextExpr(j.Parents.Name)).Contains("one"))
			if _, err := rowset.CompileRead(rowset.Read(rowset.FromModel(j.Parents.Query().Where(p)))); !errors.Is(err, fault.Invalid) {
				t.Fatal("target query lost construction error", err)
			}
			validRelation := j.TargetParent.Where(p)
			if err := orm.Check(validRelation); !errors.Is(err, fault.Invalid) {
				t.Fatal("valid relation lost transferred construction error", err)
			}
			g := &goldenCapture{reply: joinedReply}
			db := g.open(t.Context())
			defer db.Close(t.Context())
			if _, err := orm.All(t.Context(), db, j.Parents.Query().Where(p)); !errors.Is(err, fault.Invalid) {
				t.Fatal("target query executed invalid adapted predicate", err)
			}
			if _, err := orm.All(t.Context(), db, j.Targets.Query().With(validRelation)); !errors.Is(err, fault.Invalid) {
				t.Fatal("valid relation executed invalid adapted predicate", err)
			}
			for name, predicate := range map[string]orm.Predicate[j.Target]{
				"Has":        orm.Has(invalidRelation),
				"DoesntHave": orm.Not(orm.Has(invalidRelation)),
			} {
				if _, err := rowset.CompileRead(rowset.Read(rowset.FromModel(j.Targets.Query().Where(predicate)))); !errors.Is(err, fault.Invalid) {
					t.Fatal(name+" lost construction error", err)
				}
			}
			if _, err := rowset.CompileRead(rowset.Read(rowset.FromModel(j.Targets.Query()).Where(expr.Has(invalidRelation, expr.TextRow(orm.TextExpr(j.Parents.Name)).Contains("one"))))); !errors.Is(err, fault.Invalid) {
				t.Fatal("expr.Has lost construction error", err)
			}
			if g.statements != 0 {
				t.Fatal("invalid adapted predicate reached SQL", g.statements)
			}
		})
	}

	fake := borrowedFilterTarget{Relation: j.Peers}
	rejected := expr.RelationFilter(fake, expr.TextRow(orm.TextExpr(j.Parents.Name)).Contains("x"))
	if _, err := rowset.CompileRead(rowset.Read(rowset.FromModel(j.Parents.Query().Where(rejected)))); !errors.Is(err, fault.Invalid) {
		t.Fatal("borrowed target accepted", err)
	}
	var nilRelation *orm.ThroughRelation[j.Parent, j.Target, j.Pivot]
	for _, r := range []orm.ExistenceRelation[j.Parent, j.Target]{nil, nilRelation, orm.HasMany[j.Parent, j.Target]{}} {
		p := expr.RelationFilter(r, expr.TextRow(orm.TextExpr(j.Targets.Name)).Contains("x"))
		if _, err := rowset.CompileRead(rowset.Read(rowset.FromModel(j.Targets.Query().Where(p)))); !errors.Is(err, fault.Invalid) {
			t.Fatal("nil/zero relation accepted", err)
		}
	}
}
