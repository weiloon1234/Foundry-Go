package query

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

type relationSpec[M, N any] struct {
	// hop is the intermediate model of HasManyThrough/HasOneThrough.
	hop *relationHop
	// morph limits a MorphTo to parents storing one morph name.
	morph          *morphSource
	scope          func(context.Context, Query[M], Query[N], M) (Query[N], bool, error)
	name           string
	source         Query[M]
	target         Query[N]
	local, foreign fieldRef
	fetch          func(context.Context, database.Executor, Query[M], Query[N], []M, *relationLoadState, int) ([][]N, error)
	bindingError   error
	// declarationError is set by a constructor (for example an invalid morph
	// name) and, unlike bindingError, survives generated Bind calls.
	declarationError error
}

func (q Query[M]) hasQueryOptions() bool {
	return q.softDeleteScope != activeRecords || len(q.predicates) != 0 || len(q.orders) != 0 || len(q.relations) != 0 || q.limit.IsSet() || q.offset != 0 || q.relationLimits != nil ||
		len(q.withoutScopes) != 0 || q.allScopesOff || q.skipModelHooks
}

func (s relationSpec[M, N]) bind(name string, source Query[M], target Query[N]) relationSpec[M, N] {
	s.bindingError = nil
	if source.hasQueryOptions() || target.hasQueryOptions() {
		s.bindingError = fault.New(fault.Invalid, "relation Bind requires bare metadata queries; apply scopes to the relation descriptor")
	}
	s.name = name
	s.source = source
	s.target.table = target.table
	s.target.definition = target.definition
	return s
}

func (s relationSpec[M, N]) validate(source string, depth int, limits RelationLimits) error {
	if s.fetch == nil {
		return fault.New(fault.Invalid, "relation requires typed key declarations")
	}
	return s.validateMetadata(source, depth, limits)
}

func (s relationSpec[M, N]) validateMetadata(source string, depth int, limits RelationLimits) error {
	if s.declarationError != nil {
		return s.declarationError
	}
	if s.bindingError != nil {
		return s.bindingError
	}
	if depth > limits.MaxDepth {
		return fault.New(fault.Invalid, "relation nesting exceeds its depth bound")
	}
	if s.name == "" || s.source.definition == nil || s.target.definition == nil {
		return fault.New(fault.Invalid, "relation requires generated binding metadata")
	}
	if err := s.source.validateCore(); err != nil {
		return err
	}
	if err := s.local.validate(source); err != nil {
		return err
	}
	if err := s.local.validate(s.source.table); err != nil {
		return err
	}
	if err := s.foreign.validate(s.target.table); err != nil {
		return err
	}
	if _, ok := s.source.definition.modelField(s.local.column); !ok {
		return fault.New(fault.Invalid, "relation source key is not a generated model field")
	}
	if _, ok := s.target.definition.modelField(s.foreign.column); !ok {
		return fault.New(fault.Invalid, "relation target key is not a generated model field")
	}
	if s.target.limit.IsSet() || s.target.offset != 0 {
		return fault.New(fault.Invalid, "relation scopes cannot use global limits or offsets")
	}
	if s.hop != nil {
		if err := s.hop.validate(); err != nil {
			return err
		}
	}
	return s.target.validateAt(depth, limits)
}

// OneRelation loads at most one target per parent. A second target for the same
// key is a cardinality error; no arbitrary first row is selected.
type OneRelation[M, N any] struct {
	spec relationSpec[M, N]
	get  func(M) relation.One[N]
	set  func(M, relation.One[N]) M
	// ofMany selects one target among several; see OfMany.
	ofMany ofManyChoice
}

// ManyRelation loads an explicitly ordered collection for each parent key.
type ManyRelation[M, N any] struct {
	spec relationSpec[M, N]
	get  func(M) relation.Many[N]
	set  func(M, relation.Many[N]) M
}

// BelongsTo connects compatible typed keys, including a nullable source key.
// Generated binding supplies model metadata and the typed relation slot.
func BelongsTo[M, N any, K comparable](local KeyField[M, K], foreign KeyField[N, K]) OneRelation[M, N] {
	return OneRelation[M, N]{spec: directSpec(local, foreign)}
}
func HasOne[M, N any, K comparable](local KeyField[M, K], foreign KeyField[N, K]) OneRelation[M, N] {
	return OneRelation[M, N]{spec: directSpec(local, foreign)}
}
func HasMany[M, N any, K comparable](local KeyField[M, K], foreign KeyField[N, K]) ManyRelation[M, N] {
	return ManyRelation[M, N]{spec: directSpec(local, foreign)}
}

func directSpec[M, N any, K comparable](local KeyField[M, K], foreign KeyField[N, K]) relationSpec[M, N] {
	if nilDescriptor(local) || nilDescriptor(foreign) {
		return relationSpec[M, N]{}
	}
	a, b := local.relationKey(), foreign.relationKey()
	return relationSpec[M, N]{local: a.ref, foreign: b.ref, scope: func(ctx context.Context, source Query[M], target Query[N], parent M) (Query[N], bool, error) {
		if err := ctx.Err(); err != nil {
			return Query[N]{}, false, err
		}
		metadata, _ := source.definition.modelField(a.ref.column)
		selected, err := readRelationKey(metadata, a, parent)
		if err != nil {
			return Query[N]{}, false, err
		}
		key, present := selected.Get()
		if !present {
			return Query[N]{}, false, nil
		}
		return target.Where(b.Eq(key.value)), true, nil
	}, fetch: func(ctx context.Context, executor database.Executor, source Query[M], target Query[N], parents []M, state *relationLoadState, depth int) ([][]N, error) {
		return fetchDirect(ctx, executor, source, target, a, b, parents, state, depth)
	}}
}

// Bind is a declaration boundary used by generated code. Source and target
// contribute model metadata; relation scopes are declared with Where/With.
func (r OneRelation[M, N]) Bind(name string, source Query[M], target Query[N], get func(M) relation.One[N], set func(M, relation.One[N]) M) OneRelation[M, N] {
	r.spec = r.spec.bind(name, source, target)
	r.get = get
	r.set = set
	return r
}
func (r ManyRelation[M, N]) Bind(name string, source Query[M], target Query[N], get func(M) relation.Many[N], set func(M, relation.Many[N]) M) ManyRelation[M, N] {
	r.spec = r.spec.bind(name, source, target)
	r.get = get
	r.set = set
	return r
}
func (r OneRelation[M, N]) Where(predicates ...Predicate[N]) OneRelation[M, N] {
	r.spec.target = r.spec.target.Where(predicates...)
	return r
}
func (r ManyRelation[M, N]) Where(predicates ...Predicate[N]) ManyRelation[M, N] {
	r.spec.target = r.spec.target.Where(predicates...)
	return r
}
func (r OneRelation[M, N]) OrderBy(orders ...Order[N]) OneRelation[M, N] {
	r.spec.target = r.spec.target.OrderBy(orders...)
	return r
}
func (r ManyRelation[M, N]) OrderBy(orders ...Order[N]) ManyRelation[M, N] {
	r.spec.target = r.spec.target.OrderBy(orders...)
	return r
}
func (r OneRelation[M, N]) With(children ...Relation[N]) OneRelation[M, N] {
	r.spec.target = r.spec.target.With(children...)
	return r
}
func (r ManyRelation[M, N]) With(children ...Relation[N]) ManyRelation[M, N] {
	r.spec.target = r.spec.target.With(children...)
	return r
}
func (r OneRelation[M, N]) relationName() string       { return r.spec.name }
func (r ManyRelation[M, N]) relationName() string      { return r.spec.name }
func (r OneRelation[M, N]) copyRelation() Relation[M]  { return r }
func (r ManyRelation[M, N]) copyRelation() Relation[M] { return r }
func (r OneRelation[M, N]) validateRelation(source string, depth int, limits RelationLimits) error {
	if r.get == nil || r.set == nil {
		return fault.New(fault.Invalid, "singular relation requires a typed slot")
	}
	if err := r.validateOfMany(); err != nil {
		return err
	}
	return r.spec.validate(source, depth, limits)
}
func (r ManyRelation[M, N]) validateRelation(source string, depth int, limits RelationLimits) error {
	if r.get == nil || r.set == nil {
		return fault.New(fault.Invalid, "collection relation requires a typed slot")
	}
	return r.spec.validate(source, depth, limits)
}

func selectedParents[M any](parents []M, skip func(M) bool) ([]M, []int) {
	selected := make([]M, 0, len(parents))
	indices := make([]int, 0, len(parents))
	for i, m := range parents {
		if !skip(m) {
			selected = append(selected, m)
			indices = append(indices, i)
		}
	}
	return selected, indices
}
func (r OneRelation[M, N]) loadRelation(ctx context.Context, executor database.Executor, parents []M, state *relationLoadState, depth int, missing bool) ([]M, error) {
	selected, indices := selectedParents(parents, func(m M) bool { return missing && r.get(m).IsLoaded() })
	groups, err := r.spec.fetch(ctx, executor, r.spec.source, r.oneOfManyTarget(), selected, state, depth)
	if err != nil {
		return nil, err
	}
	result := parents // owned by loadRelations, which copied the caller slice once
	for i, group := range groups {
		if len(group) > 1 {
			return nil, database.NewError("singular relation cardinality", database.TooManyRows)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := state.attach(len(group)); err != nil {
			return nil, err
		}
		var model value.Optional[N]
		if len(group) == 1 {
			model = value.Set(group[0])
		}
		index := indices[i]
		result[index] = r.set(result[index], relation.Single(model))
	}
	return result, nil
}
func (r ManyRelation[M, N]) loadRelation(ctx context.Context, executor database.Executor, parents []M, state *relationLoadState, depth int, missing bool) ([]M, error) {
	selected, indices := selectedParents(parents, func(m M) bool { return missing && r.get(m).IsLoaded() })
	groups, err := r.spec.fetch(ctx, executor, r.spec.source, r.spec.target, selected, state, depth)
	if err != nil {
		return nil, err
	}
	result := parents // owned by loadRelations, which copied the caller slice once
	for i, group := range groups {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := state.attach(len(group)); err != nil {
			return nil, err
		}
		index := indices[i]
		result[index] = r.set(result[index], relation.FoundryCollection(sqlowner.Seal{}, group))
	}
	return result, nil
}

type relationKeyValue[K comparable] struct {
	value    K
	identity cursorValue
}

func readRelationKey[M any, K comparable](metadata ModelField[M], field ScalarField[M, K], model M) (value.Optional[relationKeyValue[K]], error) {
	raw, err := metadata.get(model)
	if err != nil {
		return value.Optional[relationKeyValue[K]]{}, err
	}
	if raw == nil {
		return value.Optional[relationKeyValue[K]]{}, nil
	}
	key, err := field.codec.Decode(raw)
	if err != nil {
		return value.Optional[relationKeyValue[K]]{}, err
	}
	identity, err := metadata.equalityKey(raw)
	if err != nil {
		return value.Optional[relationKeyValue[K]]{}, err
	}
	return value.Set(relationKeyValue[K]{key, identity}), nil
}

func relationKeys[M any, K comparable](ctx context.Context, metadata ModelField[M], field ScalarField[M, K], parents []M, batchSize int) ([]value.Optional[cursorValue], []K, map[cursorValue]bool, error) {
	positions := make([]value.Optional[cursorValue], len(parents))
	keys := make([]K, 0, len(parents))
	seen := make(map[cursorValue]bool)
	for i, m := range parents {
		if i%batchSize == 0 {
			if err := ctx.Err(); err != nil {
				return nil, nil, nil, err
			}
		}
		key, err := readRelationKey(metadata, field, m)
		if err != nil {
			return nil, nil, nil, err
		}
		if k, ok := key.Get(); ok {
			positions[i] = value.Set(k.identity)
			if !seen[k.identity] {
				seen[k.identity] = true
				keys = append(keys, k.value)
			}
		}
	}
	return positions, keys, seen, nil
}

func fetchDirect[M, N any, K comparable](ctx context.Context, executor database.Executor, source Query[M], target Query[N], local ScalarField[M, K], foreign ScalarField[N, K], parents []M, state *relationLoadState, depth int) ([][]N, error) {
	localMeta, _ := source.definition.modelField(local.ref.column)
	foreignMeta, _ := target.definition.modelField(foreign.ref.column)
	positions, keys, seen, err := relationKeys(ctx, localMeta, local, parents, state.limits.BatchSize)
	if err != nil {
		return nil, err
	}
	var rows []N
	target.relationLimits = &state.limits
	// The whole relation tree was validated once before the parent read. The
	// batch SELECT does not load relations, so it compiles without revalidating
	// the nested subtree; loadRelations below reuses the same descriptors.
	fetch := target.inContext(ctx)
	fetch.relations = nil
	ordered, err := fetch.paginationBase()
	if err != nil {
		return nil, err
	}
	for offset := 0; offset < len(keys); offset += state.limits.BatchSize {
		end := min(offset+state.limits.BatchSize, len(keys))
		q := ordered.Where(foreign.In(keys[offset:end]...)).Limit(state.remaining + 1)
		batch, err := q.allRows(ctx, executor)
		if err != nil {
			return nil, err
		}
		if len(batch) > state.remaining {
			return nil, fault.New(fault.Invalid, "related rows exceed the shared loading budget")
		}
		state.remaining -= len(batch)
		rows = append(rows, batch...)
	}
	rows, err = target.loadRelations(ctx, executor, rows, state, depth, false)
	if err != nil {
		return nil, err
	}
	groups := make(map[cursorValue][]N, len(keys))
	for _, row := range rows {
		key, err := readRelationKey(foreignMeta, foreign, row)
		if err != nil {
			return nil, err
		}
		k, present := key.Get()
		if !present || !seen[k.identity] {
			return nil, fault.New(fault.Invalid, "related row key does not match requested keys; check key equality semantics")
		}
		groups[k.identity] = append(groups[k.identity], row)
	}
	result := make([][]N, len(parents))
	for i, key := range positions {
		if k, ok := key.Get(); ok {
			result[i] = groups[k]
		}
	}
	return result, nil
}
