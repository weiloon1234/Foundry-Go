package joined

import (
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:model table=joined_parents primary=ID
type Parent struct {
	ID   int64
	Name string
	// Foundry field behavior (generated): Managed soft-delete timestamp: Delete sets this stored field and Restore clears it. Ordinary queries exclude deleted models; WithTrashed and OnlyTrashed select visibility explicitly. Assigning this field directly through a draft uses ordinary create/update hooks rather than deletion/restoration events.
	DeletedAt     value.Nullable[temporal.DateTime]
	Peers         relation.Through[Target, Pivot]
	Targets       relation.Many[Target]
	Chosen        relation.One[Target]
	ChosenThrough relation.One[Target]
	MorphChildren relation.Many[Pivot]
	MorphChild    relation.One[Pivot]
}

//foundry:model table=joined_targets primary=ID
type Target struct {
	ID       int64
	ParentID int64
	Name     string
	Rank     int64
	// Foundry field behavior (generated): Managed soft-delete timestamp: Delete sets this stored field and Restore clears it. Ordinary queries exclude deleted models; WithTrashed and OnlyTrashed select visibility explicitly. Assigning this field directly through a draft uses ordinary create/update hooks rather than deletion/restoration events.
	DeletedAt value.Nullable[temporal.DateTime]
	Parent    relation.One[Parent]
	Inverse   relation.Through[Parent, Pivot]
}

//foundry:model table=joined_pivots primary=ID
type Pivot struct {
	ID       int64
	ParentID value.Nullable[int64]
	TargetID value.Nullable[int64]
	Kind     query.MorphName
	Role     string
	// Foundry field behavior (generated): Managed soft-delete timestamp: Delete sets this stored field and Restore clears it. Ordinary queries exclude deleted models; WithTrashed and OnlyTrashed select visibility explicitly. Assigning this field directly through a draft uses ordinary create/update hooks rather than deletion/restoration events.
	DeletedAt   value.Nullable[temporal.DateTime]
	Parent      relation.One[Parent]
	Target      relation.One[Target]
	MorphParent relation.One[Parent]
	MorphTarget relation.One[Target]
}

func (Parent) MorphName() query.MorphName { return "parent" }
func (Target) MorphName() query.MorphName { return "target" }

func defineParentRelations() ParentRelationSet {
	p, t, v := ParentFields(), TargetFields(), PivotFields()
	return ParentRelationSet{
		Peers: query.ManyToMany(p.ID, v.ParentID, v.TargetID, t.ID), Targets: query.HasManyThrough(p.ID, v.ParentID, v.TargetID, t.ID), ChosenThrough: query.HasOneThrough(p.ID, v.ParentID, v.TargetID, t.ID), Chosen: query.HasOne(p.ID, t.ParentID), MorphChildren: query.MorphMany(p.ID, v.ParentID, v.Kind), MorphChild: query.MorphOne(p.ID, v.ParentID, v.Kind)}
}

func defineTargetRelations() TargetRelationSet {
	p, t, v := ParentFields(), TargetFields(), PivotFields()
	return TargetRelationSet{Parent: query.BelongsTo(t.ParentID, p.ID), Inverse: query.MorphedByMany(t.ID, v.TargetID, v.Kind, v.ParentID, p.ID)}
}

func definePivotRelations() PivotRelationSet {
	p, t, v := ParentFields(), TargetFields(), PivotFields()
	return PivotRelationSet{Parent: query.BelongsTo(v.ParentID, p.ID), Target: query.BelongsTo(v.TargetID, t.ID), MorphParent: query.MorphTo(v.ParentID, v.Kind, p.ID), MorphTarget: query.MorphTo(v.ParentID, v.Kind, t.ID)}
}
