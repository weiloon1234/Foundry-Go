package slots

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

// DeclareORM registers slots and the ordinary deletion cleanup observer for an ORM owner.
// Example: slots.DeclareORM(owner,parts).
func DeclareORM[M any, K comparable](owner extensions.Owner[M, K], parts Parts) Declaration {
	parts = parts.clone()
	identity := extensions.ORMIdentity(owner)
	return Declare(owner.Registration(), parts, func(r *foundation.Registrar, pool foundation.Key[*database.DB], resolve ResolveRuntime) error {
		if err := identity.Validate(); err != nil {
			return err
		}
		name, err := ObserverName[M]()
		if err != nil {
			return err
		}
		return orm.RegisterObserver(r, pool, orm.NewDeletionObserver[M](name), func(resolver foundation.Resolver) (func() orm.Hooks[M], error) {
			runtime, err := resolve(resolver)
			if err != nil {
				return nil, err
			}
			cleanup, err := NewCleanup(runtime, owner, parts, identity.ReferenceOf)
			if err != nil {
				return nil, err
			}
			hooks := orm.Hooks[M]{Deleted: func(ctx context.Context, tx *database.Tx, changes orm.Changes[M]) error {
				return cleanup.Deleted(ctx, tx, changes.Before(), changes.Operation())
			}}
			return func() orm.Hooks[M] { return hooks }, nil
		})
	})
}
