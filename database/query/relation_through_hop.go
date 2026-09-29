package query

import (
	"context"
	"database/sql/driver"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// hopAlias names the intermediate model inside HasManyThrough statements.
const hopAlias = "foundry_through"

// relationHop is the intermediate model of HasManyThrough/HasOneThrough. Its
// query supplies the intermediate's own filters: soft deletion and scopes.
type relationHop struct {
	table         string
	columns       []Column
	predicates    func() []expression
	first, second fieldRef
	validate      func() error
}

// ModelQuery is implemented by every generated model: it returns the model's
// metadata query. HasManyThrough uses it for the intermediate model.
type ModelQuery[M any] interface{ FoundryQuery() Query[M] }

// HasManyThrough reaches targets through an intermediate model, like Laravel's
// hasManyThrough: source.local = intermediate.first and intermediate.second =
// target.foreign. For Country -> User -> Post:
//
//	query.HasManyThrough(c.ID, u.CountryID, u.ID, p.UserID)
//
// Targets load in one joined query per key batch, into an ordinary
// relation.Many slot. The intermediate's soft deletion and global scopes apply,
// and a target reached through several intermediates appears once per path.
// WhereHas, relation aggregates and RelatedValue use the same join.
func HasManyThrough[M any, I ModelQuery[I], N any, A, B comparable](local KeyField[M, A], first KeyField[I, A], second KeyField[I, B], foreign KeyField[N, B]) ManyRelation[M, N] {
	return ManyRelation[M, N]{spec: hopSpec[M, I, N](local, first, second, foreign)}
}

// HasOneThrough is HasManyThrough with at most one target per parent; a second
// target is a cardinality error, as for HasOne.
func HasOneThrough[M any, I ModelQuery[I], N any, A, B comparable](local KeyField[M, A], first KeyField[I, A], second KeyField[I, B], foreign KeyField[N, B]) OneRelation[M, N] {
	return OneRelation[M, N]{spec: hopSpec[M, I, N](local, first, second, foreign)}
}

func hopSpec[M any, I ModelQuery[I], N any, A, B comparable](local KeyField[M, A], first KeyField[I, A], second KeyField[I, B], foreign KeyField[N, B]) relationSpec[M, N] {
	if nilDescriptor(local) || nilDescriptor(first) || nilDescriptor(second) || nilDescriptor(foreign) {
		return relationSpec[M, N]{}
	}
	a, f, s, b := local.relationKey(), first.relationKey(), second.relationKey(), foreign.relationKey()
	var zero I
	intermediate := zero.FoundryQuery()
	hop := &relationHop{first: f.ref, second: s.ref, predicates: intermediate.effectivePredicates,
		validate: func() error {
			if intermediate.definition == nil || f.ref.table != intermediate.table || s.ref.table != intermediate.table {
				return fault.New(fault.Invalid, "through relation keys must belong to the intermediate model")
			}
			if _, ok := intermediate.definition.modelField(f.ref.column); !ok {
				return fault.New(fault.Invalid, "through relation intermediate key is not a generated model field")
			}
			if _, ok := intermediate.definition.modelField(s.ref.column); !ok {
				return fault.New(fault.Invalid, "through relation intermediate key is not a generated model field")
			}
			return intermediate.validateCore()
		}}
	if intermediate.definition != nil {
		hop.table, hop.columns = intermediate.table, intermediate.definition.columns
	}
	spec := relationSpec[M, N]{local: a.ref, foreign: b.ref, hop: hop}
	spec.scope = func(ctx context.Context, source Query[M], target Query[N], parent M) (Query[N], bool, error) {
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
		node := hop.node()
		node.selections = []selectItem{{expression: fieldRef{hopAlias, s.ref.column}}}
		node.predicates = append(node.predicates, requalify(f.Eq(key.value).expression, hopAlias))
		return target.Where(b.inSubquery(subquery{node: node})), true, nil
	}
	spec.fetch = func(ctx context.Context, executor database.Executor, source Query[M], target Query[N], parents []M, state *relationLoadState, depth int) ([][]N, error) {
		return fetchHop(ctx, executor, source, target, a, f, b, hop, parents, state, depth)
	}
	return spec
}

// node selects the intermediate model under hopAlias with its own filters.
func (h *relationHop) node() selectNode {
	node := selectNode{source: tableSource{table: h.table, alias: hopAlias, columns: h.columns}}
	for _, predicate := range h.predicates() {
		node.predicates = append(node.predicates, requalify(predicate, hopAlias))
	}
	return node
}

// join adds the intermediate to a target select aliased as targetAlias.
func (h *relationHop) join(node selectNode, targetAlias string, foreign fieldRef) selectNode {
	node.joins = append(node.joins, joinNode{
		source: tableSource{table: h.table, alias: hopAlias, columns: h.columns},
		on:     binaryComparison{left: fieldRef{targetAlias, foreign.column}, right: fieldRef{hopAlias, h.second.column}, operator: equal},
	})
	for _, predicate := range h.predicates() {
		node.predicates = append(node.predicates, requalify(predicate, hopAlias))
	}
	return node
}

type hopRow[N any] struct {
	model N
	key   driver.Value
}

// fetchHop loads targets joined to the intermediate, one parent-key batch per
// query, and groups each row by the intermediate's first key.
func fetchHop[M, I, N any, A, B comparable](ctx context.Context, executor database.Executor, source Query[M], target Query[N], local ScalarField[M, A], first ScalarField[I, A], foreign ScalarField[N, B], hop *relationHop, parents []M, state *relationLoadState, depth int) ([][]N, error) {
	localMeta, _ := source.definition.modelField(local.ref.column)
	positions, keys, seen, err := relationKeys(ctx, localMeta, local, parents, state.limits.BatchSize)
	if err != nil {
		return nil, err
	}
	target.relationLimits = &state.limits
	fetch := target.inContext(ctx)
	fetch.relations = nil
	ordered, err := fetch.paginationBase()
	if err != nil {
		return nil, err
	}
	columns := len(target.definition.columns)
	scan := func(row database.Row) (hopRow[N], error) {
		model, err := target.definition.scan(partitionedRow{row, 0, columns, columns + 1})
		if err != nil {
			return hopRow[N]{}, err
		}
		var key A
		if err := (partitionedRow{row, columns, 1, columns + 1}).Scan(first.codec.Scan(&key)); err != nil {
			return hopRow[N]{}, err
		}
		raw, err := first.codec.Bind(key)
		return hopRow[N]{model: model, key: raw}, err
	}
	models := target.definition.retrieval()
	lifecycle := &readLifecycle[hopRow[N]]{primaryIndex: -1, needed: models.needed,
		prepare: func(ctx context.Context, set lifecycle.Observers) (func(context.Context, database.Executor, hopRow[N]) error, error) {
			invoke, err := models.prepare(ctx, set)
			if err != nil || invoke == nil {
				return nil, err
			}
			return func(ctx context.Context, executor database.Executor, row hopRow[N]) error {
				return invoke(ctx, executor, row.model)
			}, nil
		}}
	var rows []hopRow[N]
	for offset := 0; offset < len(keys); offset += state.limits.BatchSize {
		end := min(offset+state.limits.BatchSize, len(keys))
		node := hop.join(ordered.modelSelect(), target.table, foreign.ref)
		node.predicates = append(node.predicates, requalify(first.In(keys[offset:end]...).expression, hopAlias))
		node.selections = append(node.selections, selectItem{expression: fieldRef{hopAlias, hop.first.column}, alias: "foundry_parent_key"})
		node.limit = value.Set(state.remaining + 1)
		batch, err := readResult[hopRow[N]]{node: node, scan: scan, lifecycle: lifecycle, scopeContext: ctx}.All(ctx, executor)
		if err != nil {
			return nil, err
		}
		if len(batch) > state.remaining {
			return nil, fault.New(fault.Invalid, "related rows exceed the shared loading budget")
		}
		state.remaining -= len(batch)
		rows = append(rows, batch...)
	}
	targets := make([]N, len(rows))
	for i, row := range rows {
		targets[i] = row.model
	}
	targets, err = target.loadRelations(ctx, executor, targets, state, depth, false)
	if err != nil {
		return nil, err
	}
	firstMeta := ModelField[I]{kind: first.codec.ParameterType()}
	groups := make(map[cursorValue][]N, len(keys))
	for i, row := range rows {
		identity, err := firstMeta.equalityKey(row.key)
		if err != nil {
			return nil, err
		}
		if !seen[identity] {
			return nil, fault.New(fault.Invalid, "related row key does not match requested keys; check key equality semantics")
		}
		groups[identity] = append(groups[identity], targets[i])
	}
	result := make([][]N, len(parents))
	for i, key := range positions {
		if k, ok := key.Get(); ok {
			result[i] = groups[k]
		}
	}
	return result, nil
}

// hopAggregateInput measures targets joined to the intermediate, grouped by
// the intermediate's first key under the shared through aliases.
func hopAggregateInput[M, N any](spec relationSpec[M, N], validate func(string, int, RelationLimits) error, singular bool) aggregateInput[M, N] {
	return aggregateInput[M, N]{source: spec.source, local: spec.local, inputTable: spec.target.table, inputAlias: throughTargetAlias, group: fieldRef{hopAlias, spec.hop.first.column}, singular: singular,
		validate: func(depth int, limits RelationLimits) error {
			if len(spec.target.relations) != 0 {
				return fault.New(fault.Invalid, "aggregate input cannot request eager-loaded children")
			}
			return validate(spec.source.table, depth, limits)
		},
		selectNode: func() selectNode {
			node := selectNode{source: tableSource{table: spec.target.table, alias: throughTargetAlias, columns: spec.target.definition.columns}}
			for _, predicate := range spec.target.effectivePredicates() {
				node.predicates = append(node.predicates, requalify(predicate, throughTargetAlias))
			}
			return spec.hop.join(node, throughTargetAlias, spec.foreign)
		},
	}
}
