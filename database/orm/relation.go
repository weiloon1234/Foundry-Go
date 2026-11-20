package orm

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"reflect"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/internal/modelmeta"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

type slotCore struct {
	owner  *tableCore
	target reflect.Type
	pivot  reflect.Type
	name   string
	offset uintptr
	err    error
}

// One identifies a singular model slot. Example: Buyer One[Order,User].
type One[M, N any] struct {
	onePhase [0]struct{}
	_        [0]*M
	_        [0]*N
	core     *slotCore
}

// Many identifies a collection slot. Example: Orders Many[User,Order].
type Many[M, N any] struct {
	manyPhase [0]struct{}
	_         [0]*M
	_         [0]*N
	core      *slotCore
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

// KeyLink represents a typed key-equality join edge between two models.
type KeyLink[M, N any, K comparable] struct {
	_       [0]*M
	_       [0]*N
	local   *columnData[K]
	foreign *columnData[K]
}

// JoinKeys binds a matching key pair across two models directly from their columns.
// Example: orm.JoinKeys(Users.ID, Memberships.UserID).
func JoinKeys[M, N any, K comparable, I1, P1 any, K1 ColumnKind, R1 ColumnRole, I2, P2 any, K2 ColumnKind, R2 ColumnRole](local ColumnHandle[M, K, I1, P1, K1, R1], foreign ColumnHandle[N, K, I2, P2, K2, R2]) KeyLink[M, N, K] {
	return KeyLink[M, N, K]{local: local.stored, foreign: foreign.stored}
}

type relationBudget struct {
	limits                 RelationLimits
	remaining, attachments int
}
type relationPlan struct {
	external       bool
	writeLimit     int
	aggregate      *aggregatePlan
	singular       bool
	join           *relationJoin
	morph          *relationMorph
	choice         uint8
	slot           *slotCore
	target         *tableCore
	local, foreign querycore.FieldRef
	query          queryState
	// Query-level overrides share one immutable settings value across root branches.
	limits *RelationLimits
	err    error
	loaded func(unsafe.Pointer) bool
	load   func(context.Context, database.Executor, *relationPlan, unsafe.Pointer, int, *relationBudget, int, bool) error
}

// Relation is a model-owned, immutable eager-loading descriptor. Example: q.With(OrderRelations.Buyer).
type Relation[M any] interface{ relationPlan(*M) *relationPlan }

// BelongsTo loads a singular target using a local FK. Example: OrderRelations.Buyer.
type BelongsTo[M, N any] struct {
	belongsToPhase [0]struct{}
	_              [0]*M
	_              [0]*N
	plan           *relationPlan
}

// HasOne loads at most one target by its FK. Example: UserRelations.Profile.
type HasOne[M, N any] struct {
	hasOnePhase [0]struct{}
	_           [0]*M
	_           [0]*N
	plan        *relationPlan
}

// HasMany loads target collections by their FK. Example: UserRelations.Orders.
type HasMany[M, N any] struct {
	hasManyPhase [0]struct{}
	_            [0]*M
	_            [0]*N
	plan         *relationPlan
}

func (r BelongsTo[M, N]) relationPlan(*M) *relationPlan { return r.plan }
func (r HasOne[M, N]) relationPlan(*M) *relationPlan    { return r.plan }
func (r HasMany[M, N]) relationPlan(*M) *relationPlan   { return r.plan }
func (r BelongsTo[M, N]) relationTarget(*N)             {}
func (r HasOne[M, N]) relationTarget(*N)                {}
func (r HasMany[M, N]) relationTarget(*N)               {}
func (r BelongsTo[M, N]) ormError() error               { return validateRelation(r.plan, nil) }
func (r HasOne[M, N]) ormError() error                  { return validateRelation(r.plan, nil) }
func (r HasMany[M, N]) ormError() error                 { return validateRelation(r.plan, nil) }

// BelongsToOf unifies local and target key types. Example: orm.BelongsToOf(Orders.Buyer, Orders.BuyerID, Users.ID).
func BelongsToOf[M, N any, K comparable, I1, P1 any, K1 ColumnKind, R1 ColumnRole, I2, P2 any, K2 ColumnKind, R2 ColumnRole](slot One[M, N], local ColumnHandle[M, K, I1, P1, K1, R1], foreign ColumnHandle[N, K, I2, P2, K2, R2]) BelongsTo[M, N] {
	return BelongsTo[M, N]{plan: singularRelation(slot, local.stored, foreign.stored)}
}

// HasOneOf unifies the parent key and target FK and rejects multiple matches.
// Example: orm.HasOneOf(Users.Profile, Users.ID, Profiles.UserID).
func HasOneOf[M, N any, K comparable, I1, P1 any, K1 ColumnKind, R1 ColumnRole, I2, P2 any, K2 ColumnKind, R2 ColumnRole](slot One[M, N], local ColumnHandle[M, K, I1, P1, K1, R1], foreign ColumnHandle[N, K, I2, P2, K2, R2]) HasOne[M, N] {
	return HasOne[M, N]{plan: singularRelation(slot, local.stored, foreign.stored)}
}

func singularRelation[M, N any, K comparable](slot One[M, N], local *columnData[K], foreign *columnData[K]) *relationPlan {
	p := newRelationCore(slot.core, columnCoreOf(local), columnCoreOf(foreign), reflect.TypeFor[M](), reflect.TypeFor[N]())
	p.singular = true
	if p.err == nil {
		p.loaded = func(parent unsafe.Pointer) bool {
			return (*relation.One[N])(unsafe.Add(parent, slot.core.offset)).IsLoaded()
		}
		p.load = relationLoader[M, N](local, foreign, func(parent *M, group []N) error {
			var item value.Optional[N]
			if len(group) == 1 {
				item = value.Set(group[0])
			}
			*(*relation.One[N])(unsafe.Add(unsafe.Pointer(parent), slot.core.offset)) = relation.Single(item)
			return nil
		})
	}
	return p
}

// HasManyOf unifies parent keys with target FKs. Example: orm.HasManyOf(Users.Orders, Users.ID, Orders.BuyerID).
func HasManyOf[M, N any, K comparable, I1, P1 any, K1 ColumnKind, R1 ColumnRole, I2, P2 any, K2 ColumnKind, R2 ColumnRole](slot Many[M, N], local ColumnHandle[M, K, I1, P1, K1, R1], foreign ColumnHandle[N, K, I2, P2, K2, R2]) HasMany[M, N] {
	return HasMany[M, N]{plan: manyRelation(slot, local.stored, foreign.stored)}
}

func manyRelation[M, N any, K comparable](slot Many[M, N], local *columnData[K], foreign *columnData[K]) *relationPlan {
	p := newRelationCore(slot.core, columnCoreOf(local), columnCoreOf(foreign), reflect.TypeFor[M](), reflect.TypeFor[N]())
	if p.err == nil {
		p.loaded = func(parent unsafe.Pointer) bool {
			return (*relation.Many[N])(unsafe.Add(parent, slot.core.offset)).IsLoaded()
		}
		p.load = relationLoader[M, N](local, foreign, func(parent *M, group []N) error {
			*(*relation.Many[N])(unsafe.Add(unsafe.Pointer(parent), slot.core.offset)) = relation.FoundryCollection(sqlowner.Collections(), group)
			return nil
		})
	}
	return p
}
func newRelationCore(slot *slotCore, a, b *columnCore, source, target reflect.Type) *relationPlan {
	p := &relationPlan{slot: slot}
	if slot == nil || a == nil || b == nil || a.owner == nil || b.owner == nil {
		p.err = invalid("relation requires a bound slot and typed keys")
		return p
	}
	p.target = b.owner
	p.local, p.foreign = querycore.FieldRef{Table: a.owner.model.Table, Column: a.metadata.Name}, querycore.FieldRef{Table: b.owner.model.Table, Column: b.metadata.Name}
	p.query = newQuery(p.target)
	if slot.owner == nil || slot.owner != a.owner {
		p.err = invalid("relation local key belongs to a different table declaration")
	}
	if p.target.typ != slot.target || slot.target != target || slot.owner == nil || slot.owner.typ != source {
		p.err = safeerror.Join("operation failed", p.err, invalid("relation slot/model mismatch"))
	}
	p.err = safeerror.Join("operation failed", p.err, slot.err, declarationCoreError(slot.owner), declarationCoreError(p.target))
	return p
}

func nilInterface(v any) bool {
	return v == nil || reflect.ValueOf(v).Kind() == reflect.Pointer && reflect.ValueOf(v).IsNil()
}

// Declaration-time checks must not resolve enum descriptors during package init.
func declarationCoreError(core *tableCore) error {
	if core == nil {
		return invalid("uninitialized table")
	}
	return core.err
}
