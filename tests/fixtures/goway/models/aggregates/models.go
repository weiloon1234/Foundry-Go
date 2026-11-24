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

//foundry:model table=joined_parents var=Parents key=ID identity=false
type Parent struct {
	Count         relation.Value[int64]
	OtherCount    relation.Value[int64]
	Total         relation.Value[value.Nullable[decimal.Decimal]]
	HasAny        relation.Value[bool]
	Minimum       relation.Value[value.Nullable[string]]
	ID            int64 `foundry:"member=ID,key-role=lookup"`
	Name          string
	DeletedAt     value.Nullable[temporal.DateTime] `foundry:"role=soft-delete"`
	Peers         relation.Through[Target, Pivot]
	Targets       relation.Many[Target]
	Chosen        relation.One[Target]
	MorphChildren relation.Many[Pivot]
	MorphChild    relation.One[Pivot]
}

//foundry:model table=joined_targets var=Targets key=ID identity=false
type Target struct {
	Count     relation.Value[int64]
	ID        int64 `foundry:"member=ID,key-role=lookup"`
	ParentID  int64
	Name      string
	Rank      int64
	DeletedAt value.Nullable[temporal.DateTime] `foundry:"role=soft-delete"`
	Parent    relation.One[Parent]
	Inverse   relation.Through[Parent, Pivot]
}

//foundry:model table=joined_pivots var=Pivots key=ID identity=false
type Pivot struct {
	Count       relation.Value[int64]
	ID          int64 `foundry:"member=ID,key-role=lookup"`
	ParentID    value.Nullable[int64]
	TargetID    value.Nullable[int64]
	Kind        model.MorphName `foundry:"codec=morphNameCodec"`
	Role        string
	DeletedAt   value.Nullable[temporal.DateTime] `foundry:"role=soft-delete"`
	Parent      relation.One[Parent]
	Target      relation.One[Target]
	MorphParent relation.One[Parent]
	MorphTarget relation.One[Target]
}

func (Parent) MorphName() model.MorphName { return "parent" }
func (Target) MorphName() model.MorphName { return "target" }

var Peers = orm.ManyToManyOf(Parents.Peers, orm.JoinKeys(Parents.ID, Pivots.ParentID), orm.JoinKeys(Pivots.TargetID, Targets.ID))
var TargetsThrough = orm.HasManyThroughOf(Parents.Targets, orm.JoinKeys(Parents.ID, Pivots.ParentID), orm.JoinKeys(Pivots.TargetID, Targets.ID))
var ChosenThrough = orm.HasOneThroughOf(Parents.Chosen, orm.JoinKeys(Parents.ID, Pivots.ParentID), orm.JoinKeys(Pivots.TargetID, Targets.ID))
var Chosen = orm.HasOneOf(Parents.Chosen, Parents.ID, Targets.ParentID)
var TargetParent = orm.BelongsToOf(Targets.Parent, Targets.ParentID, Parents.ID)
var PivotParent = orm.BelongsToOf(Pivots.Parent, Pivots.ParentID, Parents.ID)
var PivotTarget = orm.BelongsToOf(Pivots.Target, Pivots.TargetID, Targets.ID)
var MorphChildren = orm.MorphManyOf(Parents.MorphChildren, orm.JoinKeys(Parents.ID, Pivots.ParentID), Pivots.Kind)
var MorphChild = orm.MorphOneOf(Parents.MorphChild, orm.JoinKeys(Parents.ID, Pivots.ParentID), Pivots.Kind)
var MorphParent = orm.MorphToOf(Pivots.MorphParent, orm.JoinKeys(Pivots.ParentID, Parents.ID), Pivots.Kind)
var MorphTarget = orm.MorphToOf(Pivots.MorphTarget, orm.JoinKeys(Pivots.ParentID, Targets.ID), Pivots.Kind)
var MorphPeers = orm.MorphToManyOf(Parents.Peers, orm.JoinKeys(Parents.ID, Pivots.ParentID), Pivots.Kind, orm.JoinKeys(Pivots.TargetID, Targets.ID))
var Inverse = orm.MorphedByManyOf(Targets.Inverse, orm.JoinKeys(Targets.ID, Pivots.TargetID), Pivots.Kind, orm.JoinKeys(Pivots.ParentID, Parents.ID))

func morphNameCodec() codec.Codec[model.MorphName] { return codec.String[model.MorphName]() }
