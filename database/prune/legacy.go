package prune

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

// Model declares that the models selected by query are obsolete. Keep the
// selection's filters in the query (for example CreatedAt older than a
// cutoff computed from TransactionTime); soft-delete models are force-deleted
// within the query's visibility.
// Deprecated: use ORMModel or ORMSoftModel with a handwritten ORM declaration.
func Model[M any](name Name, selection query.Query[M], mode Mode) Target {
	return Target{name: name, mode: mode, run: func(ctx context.Context, db database.Transactor, size int) (int64, error) {
		return selection.PruneBatch(ctx, db, size, mode == Mass)
	}}
}
