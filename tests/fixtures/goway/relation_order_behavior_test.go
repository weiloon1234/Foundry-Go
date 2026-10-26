package goway_test

import (
	"errors"
	"foundry.test/goway/models"
	j "foundry.test/goway/models/joined"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"testing"
)

type orderTargetDonor struct {
	orm.HasMany[models.User, models.User]
}
type forgedOrderRelation struct {
	orm.Relation[models.User]
	orderTargetDonor
}

func inferredRelationOrder[M, N, O, K any](f func(orm.ExistenceRelation[M, N], O, K) orm.Order[N], r orm.ExistenceRelation[M, N]) orm.Order[N] {
	var o O
	var k K
	return f(r, o, k)
}
func inferredPivotOrder[M, N, P, O, K any](f func(orm.ThroughRelation[M, N, P], O, K) orm.Order[P], r orm.ThroughRelation[M, N, P]) orm.Order[P] {
	var o O
	var k K
	return f(r, o, k)
}

func TestComputedRelationOrdersFailClosed(t *testing.T) {
	r := models.UserRelations.Orders
	forged := forgedOrderRelation{Relation: r}
	bad := expr.RelationOrder[models.User, models.User](forged, expr.Negate(expr.Value(models.Users.Age)).Asc())
	if _, err := orm.Compile(models.Users.Query().OrderBy(bad)); !errors.Is(err, fault.Invalid) {
		t.Fatal("forged relation target accepted", err)
	}
	zero := inferredRelationOrder(orm.FoundryRelationOrder[models.User, models.Order], orm.ExistenceRelation[models.User, models.Order](r))
	if _, err := orm.Compile(models.Users.Query().With(r.OrderBy(zero))); !errors.Is(err, fault.Invalid) {
		t.Fatal("zero inferred seal accepted", err)
	}
	pivot := inferredPivotOrder(orm.FoundryRelationPivotOrder[j.Parent, j.Target, j.Pivot], j.Peers)
	if _, err := orm.Compile(j.Parents.Query().With(j.Peers.OrderByPivot(pivot))); !errors.Is(err, fault.Invalid) {
		t.Fatal("zero pivot seal accepted", err)
	}
	foreign := orm.Define("joined_targets", func(b orm.Builder[j.Target], m *j.Target) j.TargetFields {
		t := orm.TableOf(b, &m.ID)
		return j.TargetFields{Table: t, ID: t.Key(), ParentID: orm.NumberOf(b, &m.ParentID), Name: orm.TextOf(b, &m.Name), Rank: orm.NumberOf(b, &m.Rank), DeletedAt: orm.SoftDeleteOf(b, &m.DeletedAt), Parent: orm.OneOf(b, &m.Parent), Inverse: orm.ThroughOf(b, &m.Inverse)}
	})
	if err := orm.Check(foreign); err != nil {
		t.Fatal(err)
	}
	for _, order := range []orm.Order[j.Target]{
		expr.RelationOrder(j.Peers, expr.Negate(expr.Value(foreign.Rank)).Asc()),
		expr.RelationOrder(j.Peers, expr.Param(foreign.Rank, 2).Asc()),
		expr.RelationOrder(j.Peers, expr.Order[j.Target]{}),
		expr.RelationOrder(orm.ThroughRelation[j.Parent, j.Target, j.Pivot]{}, expr.Value(j.Targets.Rank).Asc()),
	} {
		if _, err := orm.Compile(j.Parents.Query().With(j.Peers.OrderBy(order))); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid relation order accepted", err)
		}
	}
	foreignPivot := orm.Define("joined_pivots", func(b orm.Builder[j.Pivot], m *j.Pivot) j.PivotFields {
		deleted := orm.SoftDeleteOf(b, &m.DeletedAt)
		t := orm.TableOf(b, &m.ID)
		return j.PivotFields{Table: t, ID: t.Key(), ParentID: orm.NullableNumberOf(b, &m.ParentID), TargetID: orm.NullableNumberOf(b, &m.TargetID), Kind: orm.ColumnOf(b, &m.Kind, orm.Codec(codec.String[model.MorphName]())), Role: orm.TextOf(b, &m.Role), DeletedAt: deleted, Soft: orm.SoftDeletes(deleted), Parent: orm.OneOf(b, &m.Parent), Target: orm.OneOf(b, &m.Target), MorphParent: orm.OneOf(b, &m.MorphParent), MorphTarget: orm.OneOf(b, &m.MorphTarget)}
	})
	if err := orm.Check(foreignPivot); err != nil {
		t.Fatal(err)
	}
	for _, order := range []orm.Order[j.Pivot]{expr.RelationPivotOrder(j.Peers, expr.Lower(expr.Value(foreignPivot.Role)).Asc()), expr.RelationPivotOrder(j.Peers, expr.Order[j.Pivot]{}), expr.RelationPivotOrder(orm.ThroughRelation[j.Parent, j.Target, j.Pivot]{}, expr.Value(j.Pivots.Role).Asc())} {
		if _, err := orm.Compile(j.Parents.Query().With(j.Peers.OrderByPivot(order))); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid pivot order accepted", err)
		}
	}
	if _, err := orm.Compile(j.Parents.Query().With(j.Peers)); err != nil {
		t.Fatal("adapter mutated original relation", err)
	}
}
