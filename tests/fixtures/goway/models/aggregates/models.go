package aggregates

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Parent struct {
	Count         relation.Value[int64]
	OtherCount    relation.Value[int64]
	Total         relation.Value[value.Nullable[decimal.Decimal]]
	HasAny        relation.Value[bool]
	Minimum       relation.Value[value.Nullable[string]]
	ID            int64
	Name          string
	DeletedAt     value.Nullable[temporal.DateTime]
	Peers         relation.Through[Target, Pivot]
	Targets       relation.Many[Target]
	Chosen        relation.One[Target]
	MorphChildren relation.Many[Pivot]
	MorphChild    relation.One[Pivot]
}
type Target struct {
	Count     relation.Value[int64]
	ID        int64
	ParentID  int64
	Name      string
	Rank      int64
	DeletedAt value.Nullable[temporal.DateTime]
	Parent    relation.One[Parent]
	Inverse   relation.Through[Parent, Pivot]
}
type Pivot struct {
	Count       relation.Value[int64]
	ID          int64
	ParentID    value.Nullable[int64]
	TargetID    value.Nullable[int64]
	Kind        model.MorphName
	Role        string
	DeletedAt   value.Nullable[temporal.DateTime]
	Parent      relation.One[Parent]
	Target      relation.One[Target]
	MorphParent relation.One[Parent]
	MorphTarget relation.One[Target]
}

func (Parent) MorphName() model.MorphName { return "parent" }
func (Target) MorphName() model.MorphName { return "target" }

type ParentFields struct {
	Count      orm.Value[Parent, int64]
	OtherCount orm.Value[Parent, int64]
	Total      orm.Value[Parent, value.Nullable[decimal.Decimal]]
	HasAny     orm.Value[Parent, bool]
	Minimum    orm.Value[Parent, value.Nullable[string]]
	orm.Table[Parent, int64]
	ID            orm.Key[Parent, int64]
	Name          orm.Text[Parent, string]
	DeletedAt     orm.SoftDelete[Parent, temporal.DateTime]
	Peers         orm.Through[Parent, Target, Pivot]
	Targets       orm.Many[Parent, Target]
	Chosen        orm.One[Parent, Target]
	MorphChildren orm.Many[Parent, Pivot]
	MorphChild    orm.One[Parent, Pivot]
}

var Parents = orm.Define("joined_parents", func(b orm.Builder[Parent], m *Parent) ParentFields {
	t := orm.TableOf(b, &m.ID)
	return ParentFields{Count: orm.ValueOf(b, &m.Count), OtherCount: orm.ValueOf(b, &m.OtherCount), Total: orm.ValueOf(b, &m.Total), HasAny: orm.ValueOf(b, &m.HasAny), Minimum: orm.ValueOf(b, &m.Minimum), Table: t, ID: t.Key(), Name: orm.TextOf(b, &m.Name), DeletedAt: orm.SoftDeleteOf(b, &m.DeletedAt), Peers: orm.ThroughOf(b, &m.Peers), Targets: orm.ManyOf(b, &m.Targets), Chosen: orm.OneOf(b, &m.Chosen), MorphChildren: orm.ManyOf(b, &m.MorphChildren), MorphChild: orm.OneOf(b, &m.MorphChild)}
})

type TargetFields struct {
	Count orm.Value[Target, int64]
	orm.Table[Target, int64]
	ID        orm.Key[Target, int64]
	ParentID  orm.Number[Target, int64]
	Name      orm.Text[Target, string]
	Rank      orm.Number[Target, int64]
	DeletedAt orm.SoftDelete[Target, temporal.DateTime]
	Parent    orm.One[Target, Parent]
	Inverse   orm.Through[Target, Parent, Pivot]
}

var Targets = orm.Define("joined_targets", func(b orm.Builder[Target], m *Target) TargetFields {
	t := orm.TableOf(b, &m.ID)
	return TargetFields{Count: orm.ValueOf(b, &m.Count), Table: t, ID: t.Key(), ParentID: orm.NumberOf(b, &m.ParentID), Name: orm.TextOf(b, &m.Name), Rank: orm.NumberOf(b, &m.Rank), DeletedAt: orm.SoftDeleteOf(b, &m.DeletedAt), Parent: orm.OneOf(b, &m.Parent), Inverse: orm.ThroughOf(b, &m.Inverse)}
})

type PivotFields struct {
	Count orm.Value[Pivot, int64]
	orm.Table[Pivot, int64]
	ID          orm.Key[Pivot, int64]
	ParentID    orm.NullableNumber[Pivot, int64]
	TargetID    orm.NullableNumber[Pivot, int64]
	Kind        orm.Column[Pivot, model.MorphName]
	Role        orm.Text[Pivot, string]
	DeletedAt   orm.SoftDelete[Pivot, temporal.DateTime]
	Parent      orm.One[Pivot, Parent]
	Target      orm.One[Pivot, Target]
	MorphParent orm.One[Pivot, Parent]
	MorphTarget orm.One[Pivot, Target]
}

var Pivots = orm.Define("joined_pivots", func(b orm.Builder[Pivot], m *Pivot) PivotFields {
	t := orm.TableOf(b, &m.ID)
	return PivotFields{Count: orm.ValueOf(b, &m.Count), Table: t, ID: t.Key(), ParentID: orm.NullableNumberOf(b, &m.ParentID), TargetID: orm.NullableNumberOf(b, &m.TargetID), Kind: orm.ColumnOf(b, &m.Kind, orm.Codec(codec.String[model.MorphName]())), Role: orm.TextOf(b, &m.Role), DeletedAt: orm.SoftDeleteOf(b, &m.DeletedAt), Parent: orm.OneOf(b, &m.Parent), Target: orm.OneOf(b, &m.Target), MorphParent: orm.OneOf(b, &m.MorphParent), MorphTarget: orm.OneOf(b, &m.MorphTarget)}
})

var Peers = orm.ManyToManyOf(Parents.Peers, orm.KeyExpr(Parents.ID), orm.NullableNumberExpr(Pivots.ParentID), orm.NullableNumberExpr(Pivots.TargetID), orm.KeyExpr(Targets.ID))
var TargetsThrough = orm.HasManyThroughOf(Parents.Targets, orm.KeyExpr(Parents.ID), orm.NullableNumberExpr(Pivots.ParentID), orm.NullableNumberExpr(Pivots.TargetID), orm.KeyExpr(Targets.ID))
var ChosenThrough = orm.HasOneThroughOf(Parents.Chosen, orm.KeyExpr(Parents.ID), orm.NullableNumberExpr(Pivots.ParentID), orm.NullableNumberExpr(Pivots.TargetID), orm.KeyExpr(Targets.ID))
var Chosen = orm.HasOneOf(Parents.Chosen, orm.KeyExpr(Parents.ID), orm.NumberExpr(Targets.ParentID))
var TargetParent = orm.BelongsToOf(Targets.Parent, orm.NumberExpr(Targets.ParentID), orm.KeyExpr(Parents.ID))
var PivotParent = orm.BelongsToOf(Pivots.Parent, orm.NullableNumberExpr(Pivots.ParentID), orm.KeyExpr(Parents.ID))
var PivotTarget = orm.BelongsToOf(Pivots.Target, orm.NullableNumberExpr(Pivots.TargetID), orm.KeyExpr(Targets.ID))
var MorphChildren = orm.MorphManyOf(Parents.MorphChildren, orm.KeyExpr(Parents.ID), orm.NullableNumberExpr(Pivots.ParentID), orm.ColumnExpr(Pivots.Kind))
var MorphChild = orm.MorphOneOf(Parents.MorphChild, orm.KeyExpr(Parents.ID), orm.NullableNumberExpr(Pivots.ParentID), orm.ColumnExpr(Pivots.Kind))
var MorphParent = orm.MorphToOf(Pivots.MorphParent, orm.NullableNumberExpr(Pivots.ParentID), orm.ColumnExpr(Pivots.Kind), orm.KeyExpr(Parents.ID))
var MorphTarget = orm.MorphToOf(Pivots.MorphTarget, orm.NullableNumberExpr(Pivots.ParentID), orm.ColumnExpr(Pivots.Kind), orm.KeyExpr(Targets.ID))
var MorphPeers = orm.MorphToManyOf(Parents.Peers, orm.KeyExpr(Parents.ID), orm.NullableNumberExpr(Pivots.ParentID), orm.ColumnExpr(Pivots.Kind), orm.NullableNumberExpr(Pivots.TargetID), orm.KeyExpr(Targets.ID))
var Inverse = orm.MorphedByManyOf(Targets.Inverse, orm.KeyExpr(Targets.ID), orm.NullableNumberExpr(Pivots.TargetID), orm.ColumnExpr(Pivots.Kind), orm.NullableNumberExpr(Pivots.ParentID), orm.KeyExpr(Parents.ID))
