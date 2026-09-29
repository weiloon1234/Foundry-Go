package query

import (
	"context"
	"reflect"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// Relation is a model-owned eager-loading descriptor. Implementations are
// supplied by typed relation constructors; With captures their values.
type Relation[M any] interface {
	relationName() string
	copyRelation() Relation[M]
	validateRelation(string, int, RelationLimits) error
	// loadRelation attaches loaded slots in place: loadRelations passes a
	// slice it owns, copied once from the caller, so branches never re-copy it.
	loadRelation(context.Context, database.Executor, []M, *relationLoadState, int, bool) ([]M, error)
}

// RelationLimits bounds key batches, fetched/attached related values and nesting.
// MaxRows separately caps fetched rows and attached model/scalar values across all
// branches, including expansion when explicit input parents repeat a key.
type RelationLimits struct{ BatchSize, MaxRows, MaxDepth int }

func DefaultRelationLimits() RelationLimits {
	return RelationLimits{BatchSize: 500, MaxRows: 10000, MaxDepth: 8}
}
func (l RelationLimits) validate() error {
	if l.BatchSize < 1 || l.BatchSize > 1000 || l.MaxRows < 1 || l.MaxRows > 1000000 || l.MaxDepth < 1 || l.MaxDepth > 32 {
		return fault.New(fault.Invalid, "invalid relation resource limits")
	}
	return nil
}

type relationLoadState struct {
	limits      RelationLimits
	remaining   int
	attachments int
}

func (s *relationLoadState) attach(count int) error {
	if count > s.attachments {
		return fault.New(fault.Invalid, "attached related models exceed the shared loading budget")
	}
	s.attachments -= count
	return nil
}

type invalidRelation[M any] struct{}

func (invalidRelation[M]) relationName() string        { return "" }
func (r invalidRelation[M]) copyRelation() Relation[M] { return r }
func (invalidRelation[M]) validateRelation(string, int, RelationLimits) error {
	return fault.New(fault.Invalid, "nil relation descriptor")
}
func (invalidRelation[M]) loadRelation(context.Context, database.Executor, []M, *relationLoadState, int, bool) ([]M, error) {
	return nil, fault.New(fault.Invalid, "nil relation descriptor")
}

func copyRelations[M any](relations []Relation[M]) []Relation[M] {
	result := make([]Relation[M], len(relations))
	for i, r := range relations {
		if nilDescriptor(r) {
			result[i] = invalidRelation[M]{}
			continue
		}
		result[i] = r.copyRelation()
	}
	return result
}

func nilDescriptor(v any) bool {
	return v == nil || (reflect.ValueOf(v).Kind() == reflect.Pointer && reflect.ValueOf(v).IsNil())
}

// With derives an eager-loading query, preserving the model owner. Repeated
// relation slots fail validation rather than silently overriding their scopes.
func (q Query[M]) With(relations ...Relation[M]) Query[M] {
	q.relations = append(slices.Clone(q.relations), copyRelations(relations)...)
	return q
}

func (q Query[M]) WithRelationLimits(limits RelationLimits) Query[M] {
	q.relationLimits = &limits
	return q
}
func (q Query[M]) loadLimits() RelationLimits {
	if q.relationLimits != nil {
		return *q.relationLimits
	}
	return DefaultRelationLimits()
}

func (q Query[M]) validateAt(depth int, limits RelationLimits) error {
	if err := q.validateCore(); err != nil {
		return err
	}
	if err := limits.validate(); err != nil {
		return err
	}
	if len(q.relations) > MaxExpressionNodes {
		return fault.New(fault.Invalid, "too many relation descriptors")
	}
	seen := make(map[string]bool, len(q.relations))
	for _, r := range q.relations {
		if err := r.validateRelation(q.table, depth+1, limits); err != nil {
			return err
		}
		if seen[r.relationName()] {
			return fault.New(fault.Invalid, "relation slot was registered more than once")
		}
		seen[r.relationName()] = true
	}
	return nil
}

// Load explicitly loads the query's relations onto copies of existing models.
// It does not re-fetch/filter the parents. Errors publish no partial result.
func (q Query[M]) Load(ctx context.Context, executor database.Executor, parents []M) ([]M, error) {
	return q.load(ctx, executor, parents, false)
}

// LoadMissing preserves already-loaded slots, including loaded-empty relations.
func (q Query[M]) LoadMissing(ctx context.Context, executor database.Executor, parents []M) ([]M, error) {
	return q.load(ctx, executor, parents, true)
}
func (q Query[M]) load(ctx context.Context, executor database.Executor, parents []M, missing bool) ([]M, error) {
	if err := executionContext(ctx, executor); err != nil {
		return nil, err
	}
	q = q.inContext(ctx)
	if err := q.Validate(); err != nil {
		return nil, err
	}
	if q.definition == nil {
		return nil, fault.New(fault.Invalid, "relation loading requires model metadata")
	}
	limits := q.loadLimits()
	return q.loadRelations(ctx, executor, parents, &relationLoadState{limits: limits, remaining: limits.MaxRows, attachments: limits.MaxRows}, 0, missing)
}
func (q Query[M]) loadRelations(ctx context.Context, executor database.Executor, parents []M, state *relationLoadState, depth int, missing bool) ([]M, error) {
	result := slices.Clone(parents)
	for _, r := range q.relations {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var err error
		result, err = r.loadRelation(ctx, executor, result, state, depth+1, missing)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}
