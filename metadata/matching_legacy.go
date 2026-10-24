package metadata

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database/query"
)

// Matching preserves the legacy materialized owner predicate.
// Example: oldQuery.Where(predicate), after predicate, err := declaration.Matching(ctx,m,input).
func (k Key[M, K, V]) Matching(ctx context.Context, m *Manager, input V) (query.Predicate[M], error) {
	keys, err := k.matchingKeys(ctx, m, input)
	if err != nil {
		return query.Predicate[M]{}, err
	}
	return k.definition.owner.QueryScope(keys...), nil
}
