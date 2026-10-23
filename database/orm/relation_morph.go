package orm

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// MorphName is the existing persisted discriminator type. Example: func(Post) MorphName() orm.MorphName {return "post"}.
type MorphName = query.MorphName

// Morphable declares a concrete model's stable discriminator. Example: func(Post) MorphName() orm.MorphName {return "post"}.
type Morphable = query.Morphable

func relationMorphName[M Morphable]() (MorphName, error) {
	var m M
	name := m.MorphName()
	return name, querycore.ValidateMorphName(string(name))
}
func morphTarget[N any](p *relationPlan, kind KeyColumn[N, MorphName], name MorphName, err error) *relationPlan {
	p = copyRelationPlan(p)
	p.err = errors.Join(p.err, err)
	c := relationColumn(kind)
	if c.core == nil || c.core.owner != p.target {
		p.err = errors.Join(p.err, invalid("morph type must belong to the related model"))
		return p
	}
	p.query = where(p.query, []Predicate[N]{comparison[N](c, querycore.Equal, []MorphName{name})})
	return p
}

// MorphManyOf filters targets by the source discriminator. Example: orm.MorphManyOf(Posts.Comments,Posts.ID,Comments.OwnerID,Comments.OwnerType).
func MorphManyOf[M Morphable, N any, K comparable](slot Many[M, N], local KeyColumn[M, K], id KeyColumn[N, K], kind KeyColumn[N, MorphName]) HasMany[M, N] {
	r := HasManyOf(slot, local, id)
	name, err := relationMorphName[M]()
	r.plan = morphTarget(r.plan, kind, name, err)
	return r
}

// MorphOneOf loads at most one target of the source discriminator. Example: orm.MorphOneOf(Posts.Cover,Posts.ID,Images.OwnerID,Images.OwnerType).
func MorphOneOf[M Morphable, N any, K comparable](slot One[M, N], local KeyColumn[M, K], id KeyColumn[N, K], kind KeyColumn[N, MorphName]) HasOne[M, N] {
	r := HasOneOf(slot, local, id)
	name, err := relationMorphName[M]()
	r.plan = morphTarget(r.plan, kind, name, err)
	return r
}

// MorphToOf loads one concrete target only for matching source rows. Declare one slot per target type. Example: orm.MorphToOf(Comments.Post,Comments.OwnerID,Comments.OwnerType,Posts.ID).
func MorphToOf[M any, N Morphable, K comparable](slot One[M, N], id KeyColumn[M, K], kind KeyColumn[M, MorphName], key KeyColumn[N, K]) BelongsTo[M, N] {
	r := BelongsToOf(slot, id, key)
	r.plan = copyRelationPlan(r.plan)
	name, err := relationMorphName[N]()
	c := relationColumn(kind)
	r.plan.err = errors.Join(r.plan.err, err)
	if c.core == nil || r.plan.slot == nil || c.core.owner != r.plan.slot.owner {
		r.plan.err = errors.Join(r.plan.err, invalid("morph type must belong to the source model"))
	} else {
		r.plan.morph = &relationMorph{kind: c.core, name: string(name)}
	}
	return r
}
func morphPivot[P any](p *relationPlan, kind KeyColumn[P, MorphName], name MorphName, err error) *relationPlan {
	p = copyJoined(p)
	p.err = errors.Join(p.err, err)
	c := relationColumn(kind)
	if p.join == nil || c.core == nil || c.core.owner != p.join.pivot.table {
		p.err = errors.Join(p.err, invalid("morph type must belong to the pivot model"))
		return p
	}
	p.join.pivot = where(p.join.pivot, []Predicate[P]{comparison[P](c, querycore.Equal, []MorphName{name})})
	p.join.defaults, err = relationPivotDefaults(p.join.defaults, c.core, string(name))
	p.err = errors.Join(p.err, err)
	return p
}

// MorphToManyOf filters the shared pivot by the source discriminator. Example: orm.MorphToManyOf(Posts.Tags,Posts.ID,Taggables.OwnerID,Taggables.OwnerType,Taggables.TagID,Tags.ID).
func MorphToManyOf[M Morphable, N, P any, A, B comparable](slot Through[M, N, P], local KeyColumn[M, A], pivotLocal KeyColumn[P, A], kind KeyColumn[P, MorphName], pivotForeign KeyColumn[P, B], foreign KeyColumn[N, B]) ThroughRelation[M, N, P] {
	r := ManyToManyOf(slot, local, pivotLocal, pivotForeign, foreign)
	name, err := relationMorphName[M]()
	r.plan = morphPivot(r.plan, kind, name, err)
	return r
}

// MorphedByManyOf filters the shared pivot by one concrete target discriminator. Example: orm.MorphedByManyOf(Tags.Posts,Tags.ID,Taggables.TagID,Taggables.OwnerType,Taggables.OwnerID,Posts.ID).
func MorphedByManyOf[M any, N Morphable, P any, A, B comparable](slot Through[M, N, P], local KeyColumn[M, A], pivotLocal KeyColumn[P, A], kind KeyColumn[P, MorphName], pivotForeign KeyColumn[P, B], foreign KeyColumn[N, B]) ThroughRelation[M, N, P] {
	r := ManyToManyOf(slot, local, pivotLocal, pivotForeign, foreign)
	name, err := relationMorphName[N]()
	r.plan = morphPivot(r.plan, kind, name, err)
	return r
}
