package orm

import (
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/model"
)

func relationMorphName[M model.Morphable]() (model.MorphName, error) {
	var m M
	name := m.MorphName()
	return name, querycore.ValidateMorphName(string(name))
}

func morphTarget[N any](p *relationPlan, c *columnData[model.MorphName], name model.MorphName, err error) *relationPlan {
	p = copyRelationPlan(p)
	p.err = safeerror.Join("operation failed", p.err, err)
	core := columnCoreOf(c)
	if core == nil || core.owner != p.target {
		p.err = safeerror.Join("operation failed", p.err, invalid("morph type must belong to the related model"))
		return p
	}
	p.query = where(p.query, []Predicate[N]{comparison[N](c, querycore.Equal, []model.MorphName{name}, false)})
	return p
}

// MorphManyOf filters targets by the source discriminator. Example: orm.MorphManyOf(Offices.Notes, orm.JoinKeys(Offices.Code, Notes.SubjectID), Notes.SubjectType).
func MorphManyOf[M model.Morphable, N any, K comparable, I, P any, Kind ColumnKind, R ColumnRole](slot Many[M, N], link KeyLink[M, N, K], kind ColumnHandle[N, model.MorphName, I, P, Kind, R]) HasMany[M, N] {
	r := HasMany[M, N]{plan: manyRelation(slot, link.local, link.foreign)}
	name, err := relationMorphName[M]()
	r.plan = morphTarget[N](r.plan, kind.stored, name, err)
	return r
}

// MorphOneOf loads at most one target of the source discriminator. Example: orm.MorphOneOf(slot, orm.JoinKeys(Offices.Code, Notes.SubjectID), Notes.SubjectType).
func MorphOneOf[M model.Morphable, N any, K comparable, I, P any, Kind ColumnKind, R ColumnRole](slot One[M, N], link KeyLink[M, N, K], kind ColumnHandle[N, model.MorphName, I, P, Kind, R]) HasOne[M, N] {
	r := HasOne[M, N]{plan: singularRelation(slot, link.local, link.foreign)}
	name, err := relationMorphName[M]()
	r.plan = morphTarget[N](r.plan, kind.stored, name, err)
	return r
}

// MorphToOf loads one concrete target only for matching source rows. Declare one slot per target type.
// Example: orm.MorphToOf(Notes.Office, orm.JoinKeys(Notes.SubjectID, Offices.Code), Notes.SubjectType).
func MorphToOf[M any, N model.Morphable, K comparable, I, P any, Kind ColumnKind, R ColumnRole](slot One[M, N], link KeyLink[M, N, K], kind ColumnHandle[M, model.MorphName, I, P, Kind, R]) BelongsTo[M, N] {
	r := BelongsTo[M, N]{plan: singularRelation(slot, link.local, link.foreign)}
	r.plan = copyRelationPlan(r.plan)
	name, err := relationMorphName[N]()
	c := columnCoreOf(kind.stored)
	r.plan.err = safeerror.Join("operation failed", r.plan.err, err)
	if c == nil || r.plan.slot == nil || c.owner != r.plan.slot.owner {
		r.plan.err = safeerror.Join("operation failed", r.plan.err, invalid("morph type must belong to the source model"))
	} else {
		r.plan.morph = &relationMorph{kind: c, name: string(name)}
	}
	return r
}

func morphPivot[P any](p *relationPlan, c *columnData[model.MorphName], name model.MorphName, err error) *relationPlan {
	p = copyJoined(p)
	p.err = safeerror.Join("operation failed", p.err, err)
	core := columnCoreOf(c)
	if p.join == nil || core == nil || core.owner != p.join.pivot.table {
		p.err = safeerror.Join("operation failed", p.err, invalid("morph type must belong to the pivot model"))
		return p
	}
	p.join.pivot = where(p.join.pivot, []Predicate[P]{comparison[P](c, querycore.Equal, []model.MorphName{name}, false)})
	p.join.defaults, err = relationPivotDefaults(p.join.defaults, core, string(name))
	p.err = safeerror.Join("operation failed", p.err, err)
	return p
}

// MorphToManyOf filters the shared pivot by the source discriminator.
// Example: orm.MorphToManyOf(Offices.Labels, orm.JoinKeys(Offices.Code, Labelings.LabelableID), Labelings.LabelableType, orm.JoinKeys(Labelings.LabelID, Labels.ID)).
func MorphToManyOf[M model.Morphable, N, P any, A, B comparable, I, Pr any, Kind ColumnKind, R ColumnRole](slot Through[M, N, P], parentToPivot KeyLink[M, P, A], kind ColumnHandle[P, model.MorphName, I, Pr, Kind, R], pivotToTarget KeyLink[P, N, B]) ThroughRelation[M, N, P] {
	r := ManyToManyOf(slot, parentToPivot, pivotToTarget)
	name, err := relationMorphName[M]()
	r.plan = morphPivot[P](r.plan, kind.stored, name, err)
	return r
}

// MorphedByManyOf filters the shared pivot by one concrete target discriminator.
// Example: orm.MorphedByManyOf(Labels.Offices, orm.JoinKeys(Labels.ID, Labelings.LabelID), Labelings.LabelableType, orm.JoinKeys(Labelings.LabelableID, Offices.Code)).
func MorphedByManyOf[M any, N model.Morphable, P any, A, B comparable, I, Pr any, Kind ColumnKind, R ColumnRole](slot Through[M, N, P], parentToPivot KeyLink[M, P, A], kind ColumnHandle[P, model.MorphName, I, Pr, Kind, R], pivotToTarget KeyLink[P, N, B]) ThroughRelation[M, N, P] {
	r := ManyToManyOf(slot, parentToPivot, pivotToTarget)
	name, err := relationMorphName[N]()
	r.plan = morphPivot[P](r.plan, kind.stored, name, err)
	return r
}
