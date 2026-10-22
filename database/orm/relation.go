package orm

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/internal/modelmeta"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/internal/sqlvalue"
	"github.com/weiloon1234/Foundry-Go/value"
)

type slotCore struct {
	owner  *tableCore
	target reflect.Type
	name   string
	offset uintptr
	err    error
}

// One identifies a singular model slot. Example: Buyer One[Order,User].
type One[M, N any] struct {
	_    [0]*M
	_    [0]*N
	core *slotCore
}

// Many identifies a collection slot. Example: Orders Many[User,Order].
type Many[M, N any] struct {
	_    [0]*M
	_    [0]*N
	core *slotCore
}

// OneOf binds a singular relation slot during Define. Example: OneOf(b,&o.Buyer).
func OneOf[M, N any](b Builder[M], ptr *relation.One[N]) One[M, N] {
	return One[M, N]{core: bindSlot(b.state, unsafe.Pointer(ptr), reflect.TypeFor[relation.One[N]](), reflect.TypeFor[N]())}
}

// ManyOf binds a collection slot during Define. Example: ManyOf(b,&u.Orders).
func ManyOf[M, N any](b Builder[M], ptr *relation.Many[N]) Many[M, N] {
	return Many[M, N]{core: bindSlot(b.state, unsafe.Pointer(ptr), reflect.TypeFor[relation.Many[N]](), reflect.TypeFor[N]())}
}
func bindSlot(d *declaration, ptr unsafe.Pointer, typ, target reflect.Type) *slotCore {
	s := &slotCore{target: target}
	if d == nil || d.sealed {
		s.err = invalid("relation slots must be bound inside Define")
		return s
	}
	s.owner = d.core
	index, err := modelmeta.FieldAt(d.typ, d.template, ptr, typ)
	if err != nil {
		d.fail("relation", err.Error())
		s.err = err
		return s
	}
	f := d.typ.Field(index)
	s.name = f.Name
	s.offset = f.Offset
	if d.core.slots[index] != nil {
		d.fail(f.Name, "relation slot is already declared")
	}
	tags, err := modelmeta.ParseTags(string(f.Tag))
	if err != nil {
		d.fail(f.Name, err.Error())
	}
	if _, ok := tags["foundry"]; ok {
		d.fail(f.Name, "relation slots cannot have persistence tags")
	}
	d.core.slots[index] = s
	return s
}

// KeyColumn retains comparable relation key types, including nullable FKs.
// Example: BelongsToOf(Orders.Buyer,Orders.BuyerID,Users.ID).
type KeyColumn[M any, K comparable] interface{ ormColumn() Column[M, K] }

func (c Column[M, V]) ormColumn() Column[M, V] { return c }

type relationBudget struct{ remaining, attachments int }
type relationPlan struct {
	singular bool
	slot     *slotCore
	target   *tableCore
	err      error
	load     func(context.Context, database.Executor, unsafe.Pointer, int, *relationBudget) error
}

// Relation is a model-owned, immutable eager-loading descriptor. Example: q.With(OrderRelations.Buyer).
type Relation[M any] interface{ relationPlan(*M) *relationPlan }

// BelongsTo loads a singular target using a local FK. Example: OrderRelations.Buyer.
type BelongsTo[M, N any] struct {
	_    [0]*M
	_    [0]*N
	plan *relationPlan
}

// HasMany loads target collections by their FK. Example: UserRelations.Orders.
type HasMany[M, N any] struct {
	_    [0]*M
	_    [0]*N
	plan *relationPlan
}

func (r BelongsTo[M, N]) relationPlan(*M) *relationPlan { return r.plan }
func (r HasMany[M, N]) relationPlan(*M) *relationPlan   { return r.plan }
func (r BelongsTo[M, N]) ormError() error               { return validateRelation(r.plan, nil) }
func (r HasMany[M, N]) ormError() error                 { return validateRelation(r.plan, nil) }

// BelongsToOf unifies local and target key types. Example: BelongsToOf(Orders.Buyer,Orders.BuyerID,Users.ID).
func BelongsToOf[M, N any, K comparable](slot One[M, N], local KeyColumn[M, K], foreign KeyColumn[N, K]) BelongsTo[M, N] {
	p := newRelation(slot.core, local, foreign)
	p.singular = true
	if p.err == nil {
		p.load = relationLoader[M, N](p, local.ormColumn(), foreign.ormColumn(), func(parent *M, group []N) error {
			var item value.Optional[N]
			if len(group) == 1 {
				item = value.Set(group[0])
			}
			*(*relation.One[N])(unsafe.Add(unsafe.Pointer(parent), slot.core.offset)) = relation.Single(item)
			return nil
		})
	}
	return BelongsTo[M, N]{plan: p}
}

// HasManyOf unifies parent keys with target FKs. Example: HasManyOf(Users.Orders,Users.ID,Orders.BuyerID).
func HasManyOf[M, N any, K comparable](slot Many[M, N], local KeyColumn[M, K], foreign KeyColumn[N, K]) HasMany[M, N] {
	p := newRelation(slot.core, local, foreign)
	if p.err == nil {
		p.load = relationLoader[M, N](p, local.ormColumn(), foreign.ormColumn(), func(parent *M, group []N) error {
			*(*relation.Many[N])(unsafe.Add(unsafe.Pointer(parent), slot.core.offset)) = relation.FoundryCollection(sqlowner.Seal{}, group)
			return nil
		})
	}
	return HasMany[M, N]{plan: p}
}
func newRelation[M, N any, K comparable](slot *slotCore, local KeyColumn[M, K], foreign KeyColumn[N, K]) *relationPlan {
	p := &relationPlan{slot: slot}
	if slot == nil || nilInterface(local) || nilInterface(foreign) {
		p.err = invalid("relation requires a bound slot and typed keys")
		return p
	}
	a, b := local.ormColumn(), foreign.ormColumn()
	if a.core == nil || b.core == nil {
		p.err = invalid("relation requires bound keys")
		return p
	}
	p.target = b.core.owner
	if slot.owner == nil || slot.owner != a.core.owner {
		p.err = invalid("relation local key belongs to a different table declaration")
	}
	if p.target == nil || p.target.typ != slot.target || slot.target != reflect.TypeFor[N]() || (slot.owner == nil || slot.owner.typ != reflect.TypeFor[M]()) {
		p.err = errors.Join(p.err, invalid("relation slot/model mismatch"))
	}
	p.err = errors.Join(p.err, slot.err, declarationCoreError(slot.owner), declarationCoreError(p.target))
	return p
}
func nilInterface(v any) bool {
	return v == nil || reflect.ValueOf(v).Kind() == reflect.Pointer && reflect.ValueOf(v).IsNil()
}
func validateRelation(p *relationPlan, owner *tableCore) error {
	if p == nil {
		return invalid("nil relation descriptor")
	}
	if p.err != nil {
		return p.err
	}
	if p.slot == nil || p.load == nil {
		return invalid("relation requires a bound slot and loader")
	}
	if owner != nil && owner != p.slot.owner {
		return invalid("relation belongs to a different table declaration")
	}
	if err := errors.Join(tableError(p.slot.owner), tableError(p.target)); err != nil {
		return err
	}
	return errors.Join(newQuery(p.target).ast.Validate(), p.target.model.ValidateGlobalScopes())
}

// The typed loader is instantiated by BelongsToOf/HasManyOf at declaration time.
// Importers carry only relationPlan and invoke its function field.
func relationLoader[M, N any, K comparable](p *relationPlan, local Column[M, K], foreign Column[N, K], assign func(*M, []N) error) func(context.Context, database.Executor, unsafe.Pointer, int, *relationBudget) error {
	return func(ctx context.Context, ex database.Executor, base unsafe.Pointer, count int, budget *relationBudget) error {
		parents := unsafe.Slice((*M)(base), count)
		limits := querycore.DefaultRelationLimits()
		positions, unique, seen, err := querycore.RelationKeys(ctx, count, limits.BatchSize, func(i int) (K, sqlvalue.Value, bool, error) {
			return readRelationKey(local, unsafe.Pointer(&parents[i]))
		})
		if err != nil {
			return err
		}
		target := newQuery(p.target)
		target.ast.ScopeContext = ctx
		if err := validateQuery(target); err != nil {
			return err
		}
		target.ast, err = target.ast.StableOrder()
		if err != nil {
			return err
		}
		var rows []N
		err = querycore.FetchRelationBatches(len(unique), limits.BatchSize, &budget.remaining, func(start, end, limit int) (int, error) {
			q := where(target, []Predicate[N]{foreign.In(unique[start:end]...)})
			q.ast.Limit = value.Set(limit)
			batch, err := allRows[N](ctx, ex, q)
			if err != nil {
				return 0, err
			}
			rows = append(rows, batch...)
			return len(batch), nil
		})
		if err != nil {
			return err
		}
		groups, err := querycore.GroupRelationRows(rows, seen, func(row N) (sqlvalue.Value, bool, error) {
			_, identity, present, err := readRelationKey(foreign, unsafe.Pointer(&row))
			return identity, present, err
		})
		if err != nil {
			return err
		}
		for i, position := range positions {
			key, _ := position.Get()
			group := groups[key]
			if p.singular && len(group) > 1 {
				return database.NewError("singular relation cardinality", database.TooManyRows)
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := querycore.ConsumeRelationBudget(&budget.attachments, len(group), true); err != nil {
				return err
			}
			if err := assign(&parents[i], group); err != nil {
				return err
			}
		}
		return nil
	}
}
func readRelationKey[M, V any](column Column[M, V], base unsafe.Pointer) (V, sqlvalue.Value, bool, error) {
	raw, err := column.core.get(base)
	if err != nil || raw == nil {
		return *new(V), sqlvalue.Value{}, false, err
	}
	v, err := column.decode(raw)
	if err != nil {
		return v, sqlvalue.Value{}, false, err
	}
	identity, err := querycore.RecordEqualityKey(column.core.kind, raw)
	return v, identity, true, err
}

// With adds batched eager loading. Example: Orders.Query().With(OrderRelations.Buyer).
func (q Query[M]) With(rels ...Relation[M]) Query[M] {
	q.state.relations = slices.Clone(q.state.relations)
	for _, r := range rels {
		if nilInterface(r) {
			q.state.relations = append(q.state.relations, nil)
		} else {
			q.state.relations = append(q.state.relations, r.relationPlan(nil))
		}
	}
	return q
}
func validateRelations(q queryState) error {
	if len(q.relations) == 0 {
		return nil
	}
	owner := q.table
	if len(q.relations) > querycore.MaxExpressionNodes {
		return invalid("too many relation descriptors")
	}
	seen := map[string]bool{}
	for _, p := range q.relations {
		if err := validateRelation(p, owner); err != nil {
			return err
		}
		if owner == nil {
			owner = p.slot.owner
		}
		if seen[p.slot.name] {
			return invalid("relation slot was registered more than once")
		}
		seen[p.slot.name] = true
	}
	return nil
}
func loadPlans(ctx context.Context, ex database.Executor, base unsafe.Pointer, count int, plans []*relationPlan) error {
	limits := querycore.DefaultRelationLimits()
	budget := relationBudget{limits.MaxRows, limits.MaxRows}
	for _, p := range plans {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := p.load(ctx, ex, base, count, &budget); err != nil {
			return err
		}
	}
	return nil
}

// Load returns copies with loaded slots and discards partial results on error.
// Example: loaded,err := Load(ctx,db,orders,OrderRelations.Buyer).
func Load[M any](ctx context.Context, ex database.Executor, parents []M, rels ...Relation[M]) ([]M, error) {
	if err := executionContext(ctx, ex); err != nil {
		return nil, err
	}
	q := Query[M]{}.With(rels...)
	if err := validateRelations(q.state); err != nil {
		return nil, err
	}
	result := slices.Clone(parents)
	if err := loadPlans(ctx, ex, unsafe.Pointer(unsafe.SliceData(result)), len(result), q.state.relations); err != nil {
		return nil, err
	}
	return result, nil
}

// Declaration-time checks must not resolve enum descriptors during package init.
func declarationCoreError(core *tableCore) error {
	if core == nil {
		return invalid("uninitialized table")
	}
	return core.err
}
