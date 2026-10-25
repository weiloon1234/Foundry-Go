package metadata

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/extensions"
)

// Matching materializes a bounded ORM owner predicate for an exact canonical JSON value and declared version.
// It uses the manager's existing matching limits, validation and cancellation.
// The declaration must have an ORM owner. No match produces a false predicate;
// the subsequent owner query retains its own fresh visibility and authorization.
// This is a snapshot of matching keys, not a live subquery.
// Example: predicate, err := metadata.Matching(ctx, manager, Preferences, input); then Owners.Query().Where(predicate).
func Matching[M any, K comparable, V any](ctx context.Context, manager *Manager, key Key[M, K, V], input V) (orm.Predicate[M], error) {
	if err := key.Validate(); err != nil {
		return orm.Predicate[M]{}, err
	}
	identity := extensions.ORMIdentity(key.definition.owner)
	if err := identity.Validate(); err != nil {
		return orm.Predicate[M]{}, err
	}
	keys, err := key.matchingKeys(ctx, manager, input)
	if err != nil {
		return orm.Predicate[M]{}, err
	}
	return identity.Scope(keys...), nil
}
