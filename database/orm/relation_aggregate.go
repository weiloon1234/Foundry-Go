package orm

import (
	"database/sql/driver"
	"errors"
	"reflect"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

// Value identifies a typed computed slot. Example: OrdersCount orm.Value[User,int64].
type Value[M, V any] struct {
	_    [0]*M
	_    [0]*V
	core *slotCore
}

// ValueOf binds an aggregate slot during Define. Example: orm.ValueOf(b,&u.OrdersCount).
func ValueOf[M, V any](b Builder[M], ptr *relation.Value[V]) Value[M, V] {
	return Value[M, V]{core: bindSlot(b.state, unsafe.Pointer(ptr), reflect.TypeFor[relation.Value[V]](), reflect.TypeFor[V]())}
}

// Using binds a computation with the slot's exact owner and result type.
// Example: Users.OrdersCount.Using(expr.Related(UserRelations.Orders,expr.Count[Order]())).
func (v Value[M, V]) Using(computation AggregateRelation[M, V]) AggregateRelation[M, V] {
	return AggregateRelation[M, V]{plan: bindAggregate(v.core, computation.plan, func(base unsafe.Pointer) bool {
		return (*relation.Value[V])(unsafe.Add(base, v.core.offset)).IsLoaded()
	})}
}

// AggregateRelation loads a typed computation into a model slot.
// Example: Users.Query().With(UserAggregates.OrdersCount).
type AggregateRelation[M, V any] struct {
	_    [0]*M
	_    [0]*V
	plan *relationPlan
}

// Using replaces the computation while retaining this descriptor's slot.
// Example: UserAggregates.OrdersCount.Using(expr.Related(filtered,expr.Count[Order]())).
func (r AggregateRelation[M, V]) Using(computation AggregateRelation[M, V]) AggregateRelation[M, V] {
	if r.plan == nil {
		return AggregateRelation[M, V]{plan: &relationPlan{err: invalid("aggregate requires a bound slot")}}
	}
	p := bindAggregate(r.plan.slot, computation.plan, r.plan.loaded)
	p.err = errors.Join(r.plan.err, p.err)
	return AggregateRelation[M, V]{plan: p}
}

func (r AggregateRelation[M, V]) relationPlan(*M) *relationPlan { return r.plan }
func (r AggregateRelation[M, V]) ormError() error               { return validateRelation(r.plan, nil) }

func bindAggregate(slot *slotCore, computation *relationPlan, loaded func(unsafe.Pointer) bool) *relationPlan {
	p := copyRelationPlan(computation)
	p.slot, p.loaded, p.load = slot, loaded, loadAggregate
	if slot == nil {
		p.err = errors.Join(p.err, invalid("aggregate requires a bound slot"))
	} else {
		p.err = errors.Join(p.err, slot.err)
	}
	return p
}

// AggregateSource seals the parent and measured model of a relation computation.
// Example: func count[M,N any](r orm.AggregateSource[M,N]).
type AggregateSource[M, N any] interface{ aggregateInput(*M, *N) aggregateInput }

type aggregateInput struct {
	plan       *relationPlan
	identity   reflect.Type
	capability sqlowner.MetadataSeal
	pivot      bool
	keyScan    func() (any, func() (driver.Value, error))
}

func aggregateSource[M any](p *relationPlan, pivot bool, identity reflect.Type) aggregateInput {
	return aggregateInput{plan: p, identity: identity, capability: sqlowner.Metadata(), pivot: pivot, keyScan: func() (any, func() (driver.Value, error)) {
		m := new(M)
		column := aggregateLocal(p)
		base := unsafe.Pointer(m)
		return column.scan(base), func() (driver.Value, error) { return column.get(base) }
	}}
}

func (r BelongsTo[M, N]) aggregateInput(*M, *N) aggregateInput {
	return aggregateSource[M](r.plan, false, reflect.TypeOf(r))
}
func (r HasOne[M, N]) aggregateInput(*M, *N) aggregateInput {
	return aggregateSource[M](r.plan, false, reflect.TypeOf(r))
}
func (r HasMany[M, N]) aggregateInput(*M, *N) aggregateInput {
	return aggregateSource[M](r.plan, false, reflect.TypeOf(r))
}
func (r ThroughRelation[M, N, P]) aggregateInput(*M, *N) aggregateInput {
	return aggregateSource[M](r.plan, false, reflect.TypeOf(r))
}

type pivotAggregate[M, P any] struct {
	_    [0]*M
	_    [0]*P
	plan *relationPlan
}

func (p pivotAggregate[M, P]) aggregateInput(*M, *P) aggregateInput {
	return aggregateSource[M](p.plan, true, reflect.TypeOf(p))
}

// Pivot measures a relation's pivot while preserving both sides' scopes.
// Example: expr.Related(orm.Pivot(UserRelations.Groups),expr.Count[Membership]()).
func Pivot[M, N, P any](r ThroughRelation[M, N, P]) AggregateSource[M, P] {
	return pivotAggregate[M, P]{plan: r.plan}
}

type aggregatePlan struct {
	input   aggregateInput
	measure querycore.AggregateNode
	scan    func() (any, func(unsafe.Pointer, uintptr))
	empty   func(unsafe.Pointer, uintptr)
}

// FoundryRelated binds validated expression metadata to a sealed relation source.
// Example: orm.FoundryRelated(source,expression,column,sqlowner.Expressions()).
func FoundryRelated[M, N, V any](source AggregateSource[M, N], expression ormcore.Expression, column ormcore.Column[V], seal sqlowner.ExpressionSeal) AggregateRelation[M, V] {
	p := &relationPlan{}
	result := AggregateRelation[M, V]{plan: p}
	identity, plan := aggregateSourceIdentity(source)
	if !seal.Valid() || identity == nil {
		p.err = invalid("invalid aggregate capability or source")
		return result
	}
	input := source.aggregateInput(nil, nil)
	if !input.capability.Valid() || input.identity != identity || input.plan == nil || reflect.ValueOf(input.plan).Pointer() != plan {
		p.err = invalid("invalid aggregate source metadata")
		return result
	}
	measure, ok := expression.Node.(querycore.AggregateNode)
	p.err = errors.Join(expression.Err, column.Err)
	if !ok || column.Scan == nil {
		p.err = errors.Join(p.err, invalid("aggregate requires a native computation and scanner"))
	}
	owner := aggregateOwner(input)
	if owner == nil {
		p.err = errors.Join(p.err, invalid("aggregate requires a bound relation input"))
	} else {
		p.err = errors.Join(p.err, ormcore.CheckReferenceSet(ormcore.Source{Scope: reflect.TypeFor[N](), Owners: map[string]any{owner.model.Table: owner}}, expression.Refs, expression.ReferenceFree))
	}
	assign := func(v V) func(unsafe.Pointer, uintptr) {
		return func(base unsafe.Pointer, offset uintptr) {
			*(*relation.Value[V])(unsafe.Add(base, offset)) = relation.Computed(v)
		}
	}
	p.aggregate = &aggregatePlan{input: input, measure: measure, empty: assign(*new(V)), scan: func() (any, func(unsafe.Pointer, uintptr)) {
		var v V
		return column.Scan(&v), func(base unsafe.Pointer, offset uintptr) {
			*(*relation.Value[V])(unsafe.Add(base, offset)) = relation.Computed(v)
		}
	}}
	return result
}

// Check the concrete framework value before dispatch: embedding a sealed interface
// promotes its method, including a nil receiver. Never call such foreign methods.
// The getter must then return metadata for this exact value's plan and type.
func aggregateSourceIdentity(source any) (reflect.Type, uintptr) {
	v := reflect.ValueOf(source)
	if !v.IsValid() {
		return nil, 0
	}
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil, 0
		}
		v = v.Elem()
	}
	t := v.Type()
	if t.PkgPath() != reflect.TypeFor[relationPlan]().PkgPath() || v.Kind() != reflect.Struct {
		return nil, 0
	}
	plan := v.FieldByName("plan")
	if !plan.IsValid() || plan.Type() != reflect.TypeFor[*relationPlan]() || plan.IsNil() {
		return nil, 0
	}
	return t, plan.Pointer()
}

func aggregateOwner(input aggregateInput) *tableCore {
	if input.plan == nil {
		return nil
	}
	if input.pivot {
		if input.plan.join == nil || input.plan.join.hop {
			return nil
		}
		return input.plan.join.pivot.table
	}
	return input.plan.target
}

func aggregateLocal(p *relationPlan) *columnCore {
	if p == nil || p.slot == nil || p.slot.owner == nil {
		return nil
	}
	for _, c := range p.slot.owner.columns {
		if c.metadata.Name == p.local.Column {
			return c
		}
	}
	return nil
}

func validateAggregateMetadata(p *relationPlan, owner *tableCore) error {
	a := p.aggregate
	if p.slot == nil || p.slot.owner == nil || p.load == nil || p.loaded == nil || a == nil || a.scan == nil || a.empty == nil {
		return invalid("aggregate requires a bound slot and computation")
	}
	if owner != nil && owner != p.slot.owner {
		return invalid("aggregate belongs to a different table declaration")
	}
	r := a.input.plan
	if r == nil || r.slot == nil || r.slot.owner != p.slot.owner || aggregateLocal(r) == nil || aggregateOwner(a.input) == nil || a.input.keyScan == nil {
		return invalid("aggregate source and slot must belong to the same table declaration")
	}
	if r.choice != 0 || r.morph != nil {
		return invalid("aggregates over one-of-many or MorphTo relations are not supported")
	}
	if len(r.query.relations) != 0 || r.join != nil && len(r.join.pivot.relations) != 0 {
		return invalid("aggregate inputs cannot request eager-loaded children")
	}
	return errors.Join(p.slot.err, tableError(p.slot.owner))
}
