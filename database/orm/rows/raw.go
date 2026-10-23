package rows

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
)

// Raw hydrates exactly the schema's declared columns, matched by name in any order.
// It applies NO scopes and uses ex directly without read routing: raw SQL may write.
// Decode failure discards all results, but does not imply a write rolled back.
// Example: rows.Raw(ctx, tx, OrderRows, "SELECT id, label FROM orders WHERE id=$1", id).
func Raw[R any](ctx context.Context, ex database.Executor, schema Schema[R], sql string, args ...any) ([]R, error) {
	if ormcore.NilDescriptor(schema) {
		return nil, ormcore.Invalid("raw requires a row declaration")
	}
	core, decode := schema.rowDefinition(nil)
	if core == nil || decode == nil {
		return nil, ormcore.Invalid("uninitialized row declaration")
	}
	if err := rowError(core); err != nil {
		return nil, err
	}
	result := make([]R, 0)
	err := ormcore.Raw(ctx, ex, rawColumns(core), sql, args, func(row database.Row) error {
		item, err := decode(row)
		if err != nil {
			return err
		}
		result = append(result, item)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
func rawColumns(core *rowCore) []string {
	names := make([]string, len(core.columns))
	for i, c := range core.columns {
		names[i] = c.Name
	}
	return names
}
