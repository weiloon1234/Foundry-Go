package attachments

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/extensions"
)

// Matching materializes a bounded ORM owner predicate for ready attachments in the collection and exact locale.
// It uses the manager's existing matching limits, validation and cancellation.
// The declaration must have an ORM owner. No match produces a false predicate;
// the subsequent owner query retains its own fresh visibility and authorization.
// This is a snapshot of matching keys, not a live subquery.
// Example: predicate, err := attachments.Matching(ctx, manager, Avatar.ForLocale("en")); then Owners.Query().Where(predicate).
func Matching[M any, K comparable](ctx context.Context, manager *Manager, collection Collection[M, K]) (orm.Predicate[M], error) {
	if err := collection.Validate(); err != nil {
		return orm.Predicate[M]{}, err
	}
	identity := extensions.ORMIdentity(collection.definition.owner)
	if err := identity.Validate(); err != nil {
		return orm.Predicate[M]{}, err
	}
	keys, err := collection.matchingKeys(ctx, manager)
	if err != nil {
		return orm.Predicate[M]{}, err
	}
	return identity.Scope(keys...), nil
}
