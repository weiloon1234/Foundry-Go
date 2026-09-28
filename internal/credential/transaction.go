package credential

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/sqlscope"
)

// InSchema reuses the shared infrastructure savepoint and schema boundary.
func InSchema(ctx context.Context, tx *database.Tx, db *database.DB, schema string, fn func(*database.Tx) error) error {
	return sqlscope.InSchema(ctx, tx, db, schema, fn)
}
