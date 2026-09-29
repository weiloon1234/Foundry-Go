package query

import (
	"context"
	"database/sql/driver"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
)

// MorphName is the stored discriminator naming one model in polymorphic
// relationships. Store it in a model field of this type.
type MorphName string

// Morphable is implemented by a model that takes part in polymorphic
// relationships. Declaring the name once on the model is the typed morph map:
// relationship constructors read it from the model type, so call sites never
// pass discriminator strings.
//
//	func (Post) MorphName() query.MorphName { return "post" }
type Morphable interface{ MorphName() MorphName }

func morphName[M Morphable]() (MorphName, error) {
	var zero M
	name := zero.MorphName()
	if !sqlname.Valid(string(name)) {
		return "", fault.New(fault.Invalid, "morph names use the identifier grammar")
	}
	return name, nil
}

// MorphMany is a HasMany whose targets also store the parent's morph name:
// Post -> Comment where comment.commentable_id = post.id and
// comment.commentable_type = Post's MorphName.
func MorphMany[M Morphable, N any, K comparable](local KeyField[M, K], id KeyField[N, K], kind KeyField[N, MorphName]) ManyRelation[M, N] {
	r := HasMany(local, id)
	r.spec = morphTargetSpec[M](r.spec, kind)
	return r
}

// MorphOne is the singular MorphMany.
func MorphOne[M Morphable, N any, K comparable](local KeyField[M, K], id KeyField[N, K], kind KeyField[N, MorphName]) OneRelation[M, N] {
	r := HasOne(local, id)
	r.spec = morphTargetSpec[M](r.spec, kind)
	return r
}

func morphTargetSpec[M Morphable, N any](spec relationSpec[M, N], kind KeyField[N, MorphName]) relationSpec[M, N] {
	if nilDescriptor(kind) {
		spec.declarationError = fault.New(fault.Invalid, "polymorphic relation requires a morph type field")
		return spec
	}
	name, err := morphName[M]()
	if err != nil {
		spec.declarationError = err
		return spec
	}
	spec.target = spec.target.Where(kind.relationKey().Eq(name))
	return spec
}

// MorphTo is the inverse for one parent type: Comment -> Post where
// comment.commentable_type is Post's MorphName and commentable_id = post.id.
// Declare one slot per possible parent type (Post, Video, ...); each loads the
// parent only for rows whose stored morph name matches, and is loaded empty
// for the others. There is no untyped "any parent" slot. Aggregates over
// MorphTo are rejected.
func MorphTo[M any, N Morphable, K comparable](id KeyField[M, K], kind KeyField[M, MorphName], key KeyField[N, K]) OneRelation[M, N] {
	r := BelongsTo(id, key)
	if nilDescriptor(kind) {
		r.spec.declarationError = fault.New(fault.Invalid, "polymorphic relation requires a morph type field")
		return r
	}
	name, err := morphName[N]()
	if err != nil {
		r.spec.declarationError = err
		return r
	}
	morph := &morphSource{kind: kind.relationKey().ref, name: name}
	r.spec.morph = morph
	inner, scope := r.spec.fetch, r.spec.scope
	r.spec.fetch = func(ctx context.Context, executor database.Executor, source Query[M], target Query[N], parents []M, state *relationLoadState, depth int) ([][]N, error) {
		matching, positions, err := morphSelect(morph, source, parents)
		if err != nil {
			return nil, err
		}
		groups, err := inner(ctx, executor, source, target, matching, state, depth)
		if err != nil {
			return nil, err
		}
		result := make([][]N, len(parents))
		for i, position := range positions {
			result[position] = groups[i]
		}
		return result, nil
	}
	r.spec.scope = func(ctx context.Context, source Query[M], target Query[N], parent M) (Query[N], bool, error) {
		matches, err := morphMatches(morph, source, parent)
		if err != nil || !matches {
			return Query[N]{}, false, err
		}
		return scope(ctx, source, target, parent)
	}
	return r
}

// MorphToMany links a morphable source through a shared pivot that stores the
// source's morph name: Post -> Tag through taggables(taggable_id,
// taggable_type, tag_id). Attach and Sync fill the pivot's morph name.
func MorphToMany[M Morphable, N, P any, A, B comparable](local KeyField[M, A], pivotLocal KeyField[P, A], pivotKind KeyField[P, MorphName], pivotForeign KeyField[P, B], foreign KeyField[N, B]) ThroughRelation[M, N, P] {
	name, err := morphName[M]()
	return morphPivot(ManyToMany(local, pivotLocal, pivotForeign, foreign), pivotKind, name, err)
}

// MorphedByMany is the inverse of MorphToMany for one morphable target type:
// Tag -> Post through taggables where taggable_type is Post's MorphName.
func MorphedByMany[M any, N Morphable, P any, A, B comparable](local KeyField[M, A], pivotLocal KeyField[P, A], pivotKind KeyField[P, MorphName], pivotForeign KeyField[P, B], foreign KeyField[N, B]) ThroughRelation[M, N, P] {
	name, err := morphName[N]()
	return morphPivot(ManyToMany(local, pivotLocal, pivotForeign, foreign), pivotKind, name, err)
}

func morphPivot[M, N, P any](r ThroughRelation[M, N, P], kind KeyField[P, MorphName], name MorphName, err error) ThroughRelation[M, N, P] {
	if err == nil && nilDescriptor(kind) {
		err = fault.New(fault.Invalid, "polymorphic relation requires a morph type field")
	}
	if err != nil {
		r.spec.declarationError = err
		return r
	}
	field := kind.relationKey()
	r.pivot = r.pivot.Where(field.Eq(name))
	raw, bindErr := field.codec.Bind(name)
	if bindErr != nil {
		r.spec.declarationError = bindErr
		return r
	}
	r.pivotFixed = append(r.pivotFixed, pivotAssignment{column: field.ref.column, raw: raw})
	return r
}

// pivotAssignment is a fixed pivot value, such as a morph name, filled into
// created pivots like the relationship keys.
type pivotAssignment struct {
	column string
	raw    driver.Value
}

// morphSource restricts a MorphTo to parents storing one morph name.
type morphSource struct {
	kind fieldRef
	name MorphName
}

// morphMatches reads the parent's stored morph name through its generated
// getter; SQL NULL never matches.
func morphMatches[M any](m *morphSource, source Query[M], parent M) (bool, error) {
	field, ok := source.definition.modelField(m.kind.column)
	if !ok || field.get == nil {
		return false, fault.New(fault.Invalid, "morph type is not a generated model field")
	}
	raw, err := field.get(parent)
	if err != nil || raw == nil {
		return false, err
	}
	stored, ok := raw.(string)
	return ok && MorphName(stored) == m.name, nil
}

// morphSelect keeps the parents of this morph type and their positions.
func morphSelect[M any](m *morphSource, source Query[M], parents []M) ([]M, []int, error) {
	matching := make([]M, 0, len(parents))
	positions := make([]int, 0, len(parents))
	for i, parent := range parents {
		matches, err := morphMatches(m, source, parent)
		if err != nil {
			return nil, nil, err
		}
		if matches {
			matching = append(matching, parent)
			positions = append(positions, i)
		}
	}
	return matching, positions, nil
}
