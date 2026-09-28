package query

import (
	"context"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

type throughOrder struct {
	value             valueExpression
	descending, pivot bool
}

// ThroughRelation loads complete targets and their concrete pivot models. The
// source, target, pivot and both key types are checked by the Go compiler.
type ThroughRelation[M, N, P any] struct {
	spec                     relationSpec[M, N]
	pivot                    Query[P]
	pivotLocal, pivotForeign fieldRef
	orders                   []throughOrder
	writeLimit               value.Optional[int]
	get                      func(M) relation.Through[N, P]
	set                      func(M, relation.Through[N, P]) M
	fetch                    func(context.Context, database.Executor, ThroughRelation[M, N, P], []M, *relationLoadState, int) ([][]relation.Link[N, P], error)
}

// ManyToMany connects source -> pivot -> target using compatible generated keys.
// Nullable keys are allowed; SQL equality omits links with absent keys. Each
// distinct pivot row is retained. Attach/detach lifecycle writes are separate.
func ManyToMany[M, N, P any, A, B comparable](local KeyField[M, A], pivotLocal KeyField[P, A], pivotForeign KeyField[P, B], foreign KeyField[N, B]) ThroughRelation[M, N, P] {
	if nilDescriptor(local) || nilDescriptor(pivotLocal) || nilDescriptor(pivotForeign) || nilDescriptor(foreign) {
		return ThroughRelation[M, N, P]{}
	}
	a, pa, pb, b := local.relationKey(), pivotLocal.relationKey(), pivotForeign.relationKey(), foreign.relationKey()
	return ThroughRelation[M, N, P]{
		spec: relationSpec[M, N]{local: a.ref, foreign: b.ref}, pivotLocal: pa.ref, pivotForeign: pb.ref,
		fetch: func(ctx context.Context, executor database.Executor, r ThroughRelation[M, N, P], parents []M, state *relationLoadState, depth int) ([][]relation.Link[N, P], error) {
			return fetchThrough(ctx, executor, r, a, pa, pb, b, parents, state, depth)
		},
	}
}

// Bind supplies generated metadata and a concrete loaded slot. Metadata queries
// must be bare; scopes belong to Where/WherePivot and With/WithPivot.
func (r ThroughRelation[M, N, P]) Bind(name string, source Query[M], target Query[N], pivot Query[P], get func(M) relation.Through[N, P], set func(M, relation.Through[N, P]) M) ThroughRelation[M, N, P] {
	r.spec = r.spec.bind(name, source, target)
	if pivot.hasQueryOptions() {
		r.spec.bindingError = fault.New(fault.Invalid, "relation Bind requires bare pivot metadata")
	}
	r.pivot.table, r.pivot.definition = pivot.table, pivot.definition
	r.get, r.set = get, set
	return r
}

// Where adds target-model filters without filtering the source parent rows.
func (r ThroughRelation[M, N, P]) Where(predicates ...Predicate[N]) ThroughRelation[M, N, P] {
	r.spec.target = r.spec.target.Where(predicates...)
	return r
}

// WherePivot adds filters on the concrete pivot model for each retained link.
func (r ThroughRelation[M, N, P]) WherePivot(predicates ...Predicate[P]) ThroughRelation[M, N, P] {
	r.pivot = r.pivot.Where(predicates...)
	return r
}

// OrderBy and OrderByPivot preserve their combined call order. Target and pivot
// primary keys are appended as deterministic tie-breakers when absent.
func (r ThroughRelation[M, N, P]) OrderBy(orders ...Order[N]) ThroughRelation[M, N, P] {
	r.orders = slices.Clone(r.orders)
	for _, o := range orders {
		r.orders = append(r.orders, throughOrder{value: o.value(), descending: o.descending})
	}
	return r
}

// OrderByPivot appends pivot ordering at this position in the combined order.
func (r ThroughRelation[M, N, P]) OrderByPivot(orders ...Order[P]) ThroughRelation[M, N, P] {
	r.orders = slices.Clone(r.orders)
	for _, o := range orders {
		r.orders = append(r.orders, throughOrder{value: o.value(), descending: o.descending, pivot: true})
	}
	return r
}

// With loads nested relations on each target using the shared loading budget.
func (r ThroughRelation[M, N, P]) With(children ...Relation[N]) ThroughRelation[M, N, P] {
	r.spec.target = r.spec.target.With(children...)
	return r
}

// WithPivot loads nested pivot relations under the same depth and row limits.
func (r ThroughRelation[M, N, P]) WithPivot(children ...Relation[P]) ThroughRelation[M, N, P] {
	r.pivot = r.pivot.With(children...)
	return r
}
func (r ThroughRelation[M, N, P]) relationName() string      { return r.spec.name }
func (r ThroughRelation[M, N, P]) copyRelation() Relation[M] { return r }
func (r ThroughRelation[M, N, P]) validateRelation(source string, depth int, limits RelationLimits) error {
	if r.get == nil || r.set == nil {
		return fault.New(fault.Invalid, "many-to-many requires generated metadata, typed keys and a slot")
	}
	if err := r.validateMetadata(source, depth, limits); err != nil {
		return err
	}
	// Validate the combined SELECT, including alias scope and shared expression
	// limits, before a parent query or any relation branch performs I/O.
	_, err := r.compileThrough(nil, 1)
	return err
}
func (r ThroughRelation[M, N, P]) validateMetadata(source string, depth int, limits RelationLimits) error {
	if r.fetch == nil || r.pivot.definition == nil {
		return fault.New(fault.Invalid, "many-to-many requires generated metadata and typed keys")
	}
	if err := r.spec.validateMetadata(source, depth, limits); err != nil {
		return err
	}
	if r.pivot.limit.IsSet() || r.pivot.offset != 0 {
		return fault.New(fault.Invalid, "pivot scopes cannot use global windows")
	}
	if err := r.pivot.validateAt(depth, limits); err != nil {
		return err
	}
	for _, f := range []fieldRef{r.pivotLocal, r.pivotForeign, {r.pivot.table, r.pivot.definition.primary}} {
		if err := f.validate(r.pivot.table); err != nil {
			return err
		}
		if _, ok := r.pivot.definition.modelField(f.column); !ok {
			return fault.New(fault.Invalid, "pivot key is not a generated model field")
		}
	}
	if len(r.orders) > MaxExpressionNodes {
		return fault.New(fault.Invalid, "relation ordering exceeds its resource bound")
	}
	type orderedField struct {
		field fieldRef
		pivot bool
	}
	seen := make(map[orderedField]bool, len(r.orders))
	orderNodes := 0
	for _, o := range r.orders {
		table := r.spec.target.table
		if o.pivot {
			table = r.pivot.table
		}
		if err := validateRowValue(o.value, func(f fieldRef) error { return f.validate(table) }, 0, &orderNodes); err != nil {
			return err
		}
		field, plain := o.value.(fieldRef)
		if !plain {
			continue
		}
		key := orderedField{field, o.pivot}
		if seen[key] {
			return fault.New(fault.Invalid, "repeated relation ordering column")
		}
		seen[key] = true
	}
	return nil
}

func (r ThroughRelation[M, N, P]) loadRelation(ctx context.Context, executor database.Executor, parents []M, state *relationLoadState, depth int, missing bool) ([]M, error) {
	selected, indices := selectedParents(parents, func(m M) bool { return missing && r.get(m).IsLoaded() })
	groups, err := r.fetch(ctx, executor, r, selected, state, depth)
	if err != nil {
		return nil, err
	}
	result := slices.Clone(parents)
	for i, group := range groups {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// Every attached link holds two model values, target and pivot.
		if err := state.attach(2 * len(group)); err != nil {
			return nil, err
		}
		index := indices[i]
		result[index] = r.set(result[index], relation.Linked(group))
	}
	return result, nil
}
