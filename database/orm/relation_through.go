package orm

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"reflect"
	"unsafe"
)

// Through identifies a typed pivot slot. Example: Groups orm.Through[User,Group,Membership].
type Through[M, N, P any] struct {
	_    [0]*M
	_    [0]*N
	_    [0]*P
	core *slotCore
}

// ThroughOf binds a pivot slot during Define. Example: orm.ThroughOf(b,&u.Groups).
func ThroughOf[M, N, P any](b Builder[M], ptr *relation.Through[N, P]) Through[M, N, P] {
	core := bindSlot(b.state, unsafe.Pointer(ptr), reflect.TypeFor[relation.Through[N, P]](), reflect.TypeFor[N]())
	core.pivot = reflect.TypeFor[P]()
	return Through[M, N, P]{core: core}
}

// ThroughRelation retains source, target and pivot owners in a thin immutable plan.
// Example: UserRelations.Groups.WherePivot(Memberships.Role.Eq("admin")).
type ThroughRelation[M, N, P any] struct {
	_    [0]*M
	_    [0]*N
	_    [0]*P
	plan *relationPlan
}

func (r ThroughRelation[M, N, P]) relationPlan(*M) *relationPlan { return r.plan }
func (r ThroughRelation[M, N, P]) relationTarget(*N)             {}
func (r ThroughRelation[M, N, P]) ormError() error               { return validateRelation(r.plan, nil) }
func relationColumn[M any, K comparable](c KeyColumn[M, K]) columnData[K] {
	if nilInterface(c) {
		return columnData[K]{}
	}
	return c.ormColumn(nil)
}

// ManyToManyOf unifies both key pairs and binds a concrete pivot slot.
// Example: orm.ManyToManyOf(Users.Groups,Users.ID,Memberships.UserID,Memberships.GroupID,Groups.ID).
func ManyToManyOf[M, N, P any, A, B comparable](slot Through[M, N, P], local KeyColumn[M, A], pivotLocal KeyColumn[P, A], pivotForeign KeyColumn[P, B], foreign KeyColumn[N, B]) ThroughRelation[M, N, P] {
	a, pa, pb, b := relationColumn(local), relationColumn(pivotLocal), relationColumn(pivotForeign), relationColumn(foreign)
	p := newRelationCore(slot.core, a.core, b.core, reflect.TypeFor[M](), reflect.TypeFor[N]())
	if pa.core == nil || pb.core == nil || pa.core.owner == nil || pa.core.owner != pb.core.owner {
		p.err = errors.Join(p.err, invalid("pivot keys must belong to one bound model"))
	} else {
		if slot.core == nil || slot.core.pivot != reflect.TypeFor[P]() || pa.core.owner.typ != reflect.TypeFor[P]() {
			p.err = errors.Join(p.err, invalid("pivot slot/model mismatch"))
		}
		p.join = &relationJoin{pivot: newQuery(pa.core.owner), first: querycore.FieldRef{Table: pa.core.owner.model.Table, Column: pa.core.metadata.Name}, second: querycore.FieldRef{Table: pb.core.owner.model.Table, Column: pb.core.metadata.Name}}
		p.err = errors.Join(p.err, declarationCoreError(pa.core.owner))
	}
	if p.err == nil {
		p.loaded = func(base unsafe.Pointer) bool {
			return (*relation.Through[N, P])(unsafe.Add(base, slot.core.offset)).IsLoaded()
		}
		p.load = throughLoader[M, N, P](a, pa, pb, b, func(parent *M, links []relation.Link[N, P]) {
			*(*relation.Through[N, P])(unsafe.Add(unsafe.Pointer(parent), slot.core.offset)) = relation.FoundryLinked(sqlowner.Seal{}, links)
		})
	}
	return ThroughRelation[M, N, P]{plan: p}
}

// Where derives an immutable joined descriptor for its typed side. Example: links.Where(Groups.Name.Eq("maintainers")).
func (r ThroughRelation[M, N, P]) Where(ps ...Predicate[N]) ThroughRelation[M, N, P] {
	r.plan = relationWhere(r.plan, ps)
	return r
}

// OrderBy derives an immutable joined descriptor for its typed side. Example: links.OrderBy(Groups.Name.Asc()).
func (r ThroughRelation[M, N, P]) OrderBy(orders ...Order[N]) ThroughRelation[M, N, P] {
	r.plan = relationThroughOrder(r.plan, orders, false)
	return r
}

// With derives an immutable joined descriptor for its typed side. Example: links.With(GroupRelations.Owner).
func (r ThroughRelation[M, N, P]) With(children ...Relation[N]) ThroughRelation[M, N, P] {
	r.plan = relationWith(r.plan, children)
	return r
}

// WherePivot derives an immutable joined descriptor for its typed side. Example: links.WherePivot(Memberships.Role.Eq("admin")).
func (r ThroughRelation[M, N, P]) WherePivot(ps ...Predicate[P]) ThroughRelation[M, N, P] {
	r.plan = relationPivotWhere(r.plan, ps)
	return r
}

// OrderByPivot derives an immutable joined descriptor for its typed side. Example: links.OrderByPivot(Memberships.ID.Asc()).
func (r ThroughRelation[M, N, P]) OrderByPivot(orders ...Order[P]) ThroughRelation[M, N, P] {
	r.plan = relationThroughOrder(r.plan, orders, true)
	return r
}

// WithPivot derives an immutable joined descriptor for its typed side. Example: links.WithPivot(MembershipRelations.Inviter).
func (r ThroughRelation[M, N, P]) WithPivot(children ...Relation[P]) ThroughRelation[M, N, P] {
	r.plan = relationPivotWith(r.plan, children)
	return r
}

// WithTrashed sets visibility for this side only. Example: links.WithTrashed().
func (r ThroughRelation[M, N, P]) WithTrashed() ThroughRelation[M, N, P] {
	r.plan = relationVisibility(r.plan, querycore.AllRecords, false)
	return r
}

// OnlyTrashed sets visibility for this side only. Example: links.OnlyTrashed().
func (r ThroughRelation[M, N, P]) OnlyTrashed() ThroughRelation[M, N, P] {
	r.plan = relationVisibility(r.plan, querycore.TrashedRecords, false)
	return r
}

// WithoutTrashed sets visibility for this side only. Example: links.WithoutTrashed().
func (r ThroughRelation[M, N, P]) WithoutTrashed() ThroughRelation[M, N, P] {
	r.plan = relationVisibility(r.plan, querycore.ActiveRecords, false)
	return r
}

// WithTrashedPivot sets visibility for this side only. Example: links.WithTrashedPivot().
func (r ThroughRelation[M, N, P]) WithTrashedPivot() ThroughRelation[M, N, P] {
	r.plan = relationVisibility(r.plan, querycore.AllRecords, true)
	return r
}

// OnlyTrashedPivot sets visibility for this side only. Example: links.OnlyTrashedPivot().
func (r ThroughRelation[M, N, P]) OnlyTrashedPivot() ThroughRelation[M, N, P] {
	r.plan = relationVisibility(r.plan, querycore.TrashedRecords, true)
	return r
}

// WithoutTrashedPivot sets visibility for this side only. Example: links.WithoutTrashedPivot().
func (r ThroughRelation[M, N, P]) WithoutTrashedPivot() ThroughRelation[M, N, P] {
	r.plan = relationVisibility(r.plan, querycore.ActiveRecords, true)
	return r
}
