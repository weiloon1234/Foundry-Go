package orm

import (
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

// LegacyIdentityScope keeps extension predicates usable by generated consumers during migration.
// Example: legacyQuery.Where(orm.LegacyIdentityScope(identity,ids...)).
func LegacyIdentityScope[M any, K comparable](d ModelIdentity[M, K], keys ...K) query.Predicate[M] {
	if d.Validate() != nil || len(keys) > querycore.MaxIdentityBatch {
		return query.Predicate[M]{}
	}
	return query.FoundryPredicate[M](querycore.PredicateNode(d.Scope(keys...).expression), sqlowner.Queries())
}
