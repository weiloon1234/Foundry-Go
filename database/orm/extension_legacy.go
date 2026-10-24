package orm

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

// Extension adapts a legacy slot loader to With, Load and LoadMissing.
// Example: orm.Extension(Articles.Title,title.From(manager).ExtensionSlot).
func Extension[M, S any](slot Slot[M, S], source query.ExtensionSlot[M]) ExtensionRelation[M] {
	name, table, kind, err, legacyLoad := query.FoundryExtension(source, sqlowner.Queries())
	var load func(context.Context, database.Executor, []M, querycore.RelationLimits, *int, *int, bool) ([]M, error)
	if legacyLoad != nil {
		load = func(ctx context.Context, ex database.Executor, parents []M, limits querycore.RelationLimits, remaining, attached *int, missing bool) ([]M, error) {
			return legacyLoad(ctx, ex, parents, query.RelationLimits(limits), remaining, attached, missing)
		}
	}
	return extensionRelation(slot, name, table, kind, err, load)
}
