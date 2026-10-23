package orm

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
	"reflect"
	"unsafe"
)

func hopRelation[M, I, N any, A, B comparable](slot *slotCore, local KeyColumn[M, A], first KeyColumn[I, A], second KeyColumn[I, B], foreign KeyColumn[N, B]) (*relationPlan, columnData[A], columnData[A]) {
	a, f, s, b := relationColumn(local), relationColumn(first), relationColumn(second), relationColumn(foreign)
	p := newRelationCore(slot, a.core, b.core, reflect.TypeFor[M](), reflect.TypeFor[N]())
	if f.core == nil || s.core == nil || f.core.owner == nil || f.core.owner != s.core.owner {
		p.err = errors.Join(p.err, invalid("through keys must belong to one intermediate model"))
	} else {
		p.join = &relationJoin{hop: true, pivot: newQuery(f.core.owner), first: querycore.FieldRef{Table: f.core.owner.model.Table, Column: f.core.metadata.Name}, second: querycore.FieldRef{Table: s.core.owner.model.Table, Column: s.core.metadata.Name}}
		p.err = errors.Join(p.err, declarationCoreError(f.core.owner))
	}
	return p, a, f
}

// HasManyThroughOf unifies both key pairs via an intermediate model. Example: orm.HasManyThroughOf(Countries.Posts,Countries.ID,Users.CountryID,Users.ID,Posts.UserID).
func HasManyThroughOf[M, I, N any, A, B comparable](slot Many[M, N], local KeyColumn[M, A], first KeyColumn[I, A], second KeyColumn[I, B], foreign KeyColumn[N, B]) HasMany[M, N] {
	p, a, f := hopRelation(slot.core, local, first, second, foreign)
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

// HasOneThroughOf loads at most one target per source via an intermediate. Example: orm.HasOneThroughOf(Countries.LatestPost,Countries.ID,Users.CountryID,Users.ID,Posts.UserID).
func HasOneThroughOf[M, I, N any, A, B comparable](slot One[M, N], local KeyColumn[M, A], first KeyColumn[I, A], second KeyColumn[I, B], foreign KeyColumn[N, B]) HasOne[M, N] {
	p, a, f := hopRelation(slot.core, local, first, second, foreign)
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
