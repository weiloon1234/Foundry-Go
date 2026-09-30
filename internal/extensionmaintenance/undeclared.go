package extensionmaintenance

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/extensions"
)

// MaxUndeclared bounds the stored names one undeclared report reads.
const MaxUndeclared = 256

// Undeclared lists stored names in an owner's current scope that no current
// registration declares, in ascending order, for example after a model slot
// field was renamed without pinning its stored name. Truncated reports that
// the scope held more than MaxUndeclared distinct names.
type Undeclared struct {
	Names     []string `json:"names"`
	Truncated bool     `json:"truncated"`
}

// InspectUndeclared reads distinct stored names in a read-only snapshot with
// one query returning at most MaxUndeclared+1 names. PostgreSQL still scans
// the scope's rows to find them, bounded by the store's operation timeout, so
// run it as maintenance. The bound applies before declared names are removed:
// a truncated report can omit undeclared names. names never loads stored
// values; declared reports whether the manager registers a name in scope.
// Nothing is modified.
func InspectUndeclared(ctx context.Context, store *extensions.Store, owner extensions.OwnerName, names func(ctx context.Context, tx *database.Tx, scope string, limit int) ([]string, error), declared func(scope, name string) bool) (Undeclared, error) {
	if err := store.Validate(); err != nil {
		return Undeclared{}, err
	}
	if names == nil || declared == nil {
		return Undeclared{}, invalid()
	}
	scope, err := store.Registry().Scope(owner)
	if err != nil {
		return Undeclared{}, err
	}
	var result Undeclared
	err = store.Read(ctx, func(ctx context.Context, tx *database.Tx) error {
		stored, err := names(ctx, tx, scope, MaxUndeclared+1)
		if err != nil {
			return err
		}
		if len(stored) > MaxUndeclared {
			stored, result.Truncated = stored[:MaxUndeclared], true
		}
		result.Names = []string{}
		for _, name := range stored {
			if !declared(scope, name) {
				result.Names = append(result.Names, name)
			}
		}
		return nil
	})
	if err != nil {
		return Undeclared{}, err
	}
	return result, nil
}
