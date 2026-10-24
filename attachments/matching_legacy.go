package attachments

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database/query"
)

// Matching preserves the legacy materialized owner predicate.
// Example: oldQuery.Where(predicate), after predicate, err := declaration.Matching(ctx,m).
func (c Collection[M, K]) Matching(ctx context.Context, m *Manager) (query.Predicate[M], error) {
	keys, err := c.matchingKeys(ctx, m)
	if err != nil {
		return query.Predicate[M]{}, err
	}
	return c.definition.owner.QueryScope(keys...), nil
}
