package query

import (
	"context"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
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
	if slot.spec == nil {
		return "", "", nil, invalidExtensionBridge(), nil
	}
	spec := slot.spec
	return spec.name, spec.table, spec.valueType, spec.err, func(ctx context.Context, ex database.Executor, parents []M, limits RelationLimits, remaining, attachments *int, missing bool) ([]M, error) {
		if spec.err != nil {
			return nil, spec.err
		}
		if spec.load == nil {
			return nil, invalidExtensionBridge()
		}
		state := relationLoadState{limits: limits, remaining: *remaining, attachments: *attachments}
		result, err := spec.load(ctx, ex, parents, &state, missing)
		*remaining = state.remaining
		*attachments = state.attachments
		return result, err
	}
}

func invalidExtensionBridge() error { return fault.New(fault.Invalid, "invalid extension slot bridge") }

// FoundryPredicate preserves the compiler expression when adapting typed owner scopes.
// Example: query.FoundryPredicate[User](expression,sqlowner.Queries()).
func FoundryPredicate[M any](expression querycore.Expression, seal sqlowner.QuerySeal) Predicate[M] {
	if !seal.Valid() {
		return Predicate[M]{}
	}
	return Predicate[M]{expression: expression}
}
