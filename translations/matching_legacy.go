package translations

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/i18n"
)

// Matching preserves the legacy materialized owner predicate.
// Example: oldQuery.Where(predicate), after predicate, err := declaration.Matching(ctx,m,locale,text).
func (f Field[M, K]) Matching(ctx context.Context, m *Manager, locale i18n.LocaleID, text string) (query.Predicate[M], error) {
	keys, err := f.matchingKeys(ctx, m, locale, text)
	if err != nil {
		return query.Predicate[M]{}, err
	}
	return f.definition.owner.QueryScope(keys...), nil
}
