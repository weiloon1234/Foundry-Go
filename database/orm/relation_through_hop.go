package orm

import (
	"reflect"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

func hopRelation[M, I, N any, A, B comparable](slot *slotCore, firstHop KeyLink[M, I, A], secondHop KeyLink[I, N, B]) (*relationPlan, *columnData[A], *columnData[A]) {
	a, fa, fb, b := firstHop.local, firstHop.foreign, secondHop.local, secondHop.foreign
	faCore, fbCore := columnCoreOf(fa), columnCoreOf(fb)
	p := newRelationCore(slot, columnCoreOf(a), columnCoreOf(b), reflect.TypeFor[M](), reflect.TypeFor[N]())
	if faCore == nil || fbCore == nil || faCore.owner == nil || faCore.owner != fbCore.owner || faCore.owner.typ != reflect.TypeFor[I]() {
		p.err = safeerror.Join("operation failed", p.err, invalid("through keys must belong to one bound intermediate model"))
	} else {
		p.join = &relationJoin{hop: true, pivot: newQuery(faCore.owner), first: querycore.FieldRef{Table: faCore.owner.model.Table, Column: faCore.metadata.Name}, second: querycore.FieldRef{Table: fbCore.owner.model.Table, Column: fbCore.metadata.Name}}
		p.err = safeerror.Join("operation failed", p.err, declarationCoreError(faCore.owner))
	}
	return p, a, fa
}

// HasManyThroughOf unifies both key pairs via an intermediate model.
// Example: orm.HasManyThroughOf(Countries.Posts, orm.JoinKeys(Countries.ID, Users.CountryID), orm.JoinKeys(Users.ID, Posts.UserID)).
func HasManyThroughOf[M, I, N any, A, B comparable](slot Many[M, N], firstHop KeyLink[M, I, A], secondHop KeyLink[I, N, B]) HasMany[M, N] {
	p, a, f := hopRelation[M, I, N](slot.core, firstHop, secondHop)
	if p.err == nil {
		p.loaded = func(base unsafe.Pointer) bool {
			return (*relation.Many[N])(unsafe.Add(base, slot.core.offset)).IsLoaded()
		}
		p.load = relationLoader[M, N](a, f, func(parent *M, rows []N) error {
			*(*relation.Many[N])(unsafe.Add(unsafe.Pointer(parent), slot.core.offset)) = relation.FoundryCollection(sqlowner.Collections(), rows)
			return nil
		})
	}
	return HasMany[M, N]{plan: p}
}

// HasOneThroughOf loads at most one target per source via an intermediate.
// Example: orm.HasOneThroughOf(Countries.LatestPost, orm.JoinKeys(Countries.ID, Users.CountryID), orm.JoinKeys(Users.ID, Posts.UserID)).
func HasOneThroughOf[M, I, N any, A, B comparable](slot One[M, N], firstHop KeyLink[M, I, A], secondHop KeyLink[I, N, B]) HasOne[M, N] {
	p, a, f := hopRelation[M, I, N](slot.core, firstHop, secondHop)
	p.singular = true
	if p.err == nil {
		p.loaded = func(base unsafe.Pointer) bool {
			return (*relation.One[N])(unsafe.Add(base, slot.core.offset)).IsLoaded()
		}
		p.load = relationLoader[M, N](a, f, func(parent *M, rows []N) error {
			var item value.Optional[N]
			if len(rows) == 1 {
				item = value.Set(rows[0])
			}
			*(*relation.One[N])(unsafe.Add(unsafe.Pointer(parent), slot.core.offset)) = relation.Single(item)
			return nil
		})
	}
	return HasOne[M, N]{plan: p}
}
