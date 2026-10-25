package translations

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/i18n"
)

// Matching materializes a bounded ORM owner predicate for an exact localized text value.
// It uses the manager's existing matching limits, validation and cancellation.
// The declaration must have an ORM owner. No match produces a false predicate;
// the subsequent owner query retains its own fresh visibility and authorization.
// This is a snapshot of matching keys, not a live subquery.
// Example: predicate, err := translations.Matching(ctx, manager, Label, "en", "Hello"); then Owners.Query().Where(predicate).
func Matching[M any, K comparable](ctx context.Context, manager *Manager, field Field[M, K], locale i18n.LocaleID, text string) (orm.Predicate[M], error) {
	if err := field.Validate(); err != nil {
		return orm.Predicate[M]{}, err
	}
	identity := extensions.ORMIdentity(field.definition.owner)
	if err := identity.Validate(); err != nil {
		return orm.Predicate[M]{}, err
	}
	keys, err := field.matchingKeys(ctx, manager, locale, text)
	if err != nil {
		return orm.Predicate[M]{}, err
	}
	return identity.Scope(keys...), nil
}
