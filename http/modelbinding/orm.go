package modelbinding

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/value"
)

// ByColumn resolves an ORM alternate key within the supplied query's scope.
// Duplicate matches fail; no transaction or lock is acquired.
// Example: modelbinding.ByColumn(db,Users.Query(),Users.Email,emailFromPath).
func ByColumn[P, M any, K comparable](executor database.Executor, source orm.Query[M], field orm.KeyColumn[M, K], key func(P) K) Resolver[P, M] {
	return ByKey(executor, orm.Unique(source, field), key)
}

// ThroughRelation resolves a child through a direct ORM relationship.
// Example: modelbinding.ThroughRelation(db,teams,TeamRelations.Projects,Projects.Slug,slugFromPath).
func ThroughRelation[P, A, B any, K comparable, R orm.DirectRelation[A, B]](executor database.Executor, parent Resolver[P, A], relation R, field orm.KeyColumn[B, K], key func(P) K) Resolver[P, Models[A, B]] {
	return ThroughSelectedRelation(executor, parent, func(a A) A { return a }, relation, field, key)
}

// ThroughSelectedRelation extends a nested ORM binding while retaining earlier models.
// Example: modelbinding.ThroughSelectedRelation(db,projects,func(p Models[Team,Project])Project{return p.Child},ProjectRelations.Tasks,Tasks.Slug,slugFromPath).
func ThroughSelectedRelation[P, A, M, B any, K comparable, R orm.DirectRelation[M, B]](executor database.Executor, parent Resolver[P, A], selectParent func(A) M, relation R, field orm.KeyColumn[B, K], key func(P) K) Resolver[P, Models[A, B]] {
	if nilValue(executor) || selectParent == nil || key == nil {
		return Resolver[P, Models[A, B]]{err: safeerror.NewFault(fault.Invalid, "nested binding requires an executor and typed selectors")}
	}
	lookup := orm.RelatedUnique(relation, field)
	result := Then(parent, func(ctx context.Context, path P, previous A) (value.Optional[B], error) {
		return lookup.Find(ctx, executor, selectParent(previous), key(path))
	})
	result.check = func() error {
		if err := parent.Validate(); err != nil {
			return err
		}
		return lookup.Validate()
	}
	return result
}
