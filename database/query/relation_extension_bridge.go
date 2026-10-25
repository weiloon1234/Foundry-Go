package query

import (
	"context"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/extensionslot"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

// FoundryExtension lends the framework slot loader to declaration-based relations.
// The concrete descriptor prevents interface promotion from substituting callbacks.
// Example: query.FoundryExtension(slot,sqlowner.Queries()).
func FoundryExtension[M any](slot ExtensionSlot[M], seal sqlowner.QuerySeal) (string, string, reflect.Type, error, func(context.Context, database.Executor, []M, RelationLimits, *int, *int, bool) ([]M, error)) {
	if !seal.Valid() {
		return "", "", nil, invalidExtensionBridge(), nil
	}
	name, table, kind, err := slot.core.Metadata()
	if kind == nil {
		return "", "", nil, invalidExtensionBridge(), nil
	}
	return name, table, kind, err, func(ctx context.Context, ex database.Executor, parents []M, limits RelationLimits, remaining, attachments *int, missing bool) ([]M, error) {
		return slot.core.Load(ctx, ex, parents, querycore.RelationLimits(limits), remaining, attachments, missing)
	}
}

// FoundryExtensionSlot adapts the shared loader to the legacy relation API.
// Example: query.FoundryExtensionSlot(slot,sqlowner.Queries()).
func FoundryExtensionSlot[M any](slot extensionslot.Slot[M], seal sqlowner.QuerySeal) ExtensionSlot[M] {
	if !seal.Valid() {
		return ExtensionSlot[M]{}
	}
	return ExtensionSlot[M]{core: slot}
}

func invalidExtensionBridge() error { return fault.New(fault.Invalid, "invalid extension slot bridge") }

// FoundryPredicate preserves the compiler expression when adapting typed owner scopes.
// Example: query.FoundryPredicate[User](expression,sqlowner.Queries()).
func FoundryPredicate[M any](expression querycore.Expression, seal sqlowner.QuerySeal) Predicate[M] {
	if !seal.Valid() {
		return Predicate[M]{}
	}
	return Predicate[M]{expression: retainPredicate(expression)}
}
