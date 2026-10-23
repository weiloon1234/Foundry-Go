package orm

import (
	"context"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
)

// Raw hydrates the exact declared column set, matched by name in any order.
// It applies NO scopes, including soft-delete and tenant/global scopes, and uses
// ex directly without read routing: raw SQL may write. Supply bound arguments.
// Example: Orders.Raw(ctx, tx, "UPDATE orders SET total_cents=$1 RETURNING *", cents).
// Decode/open failure discards all results; this does not imply a write rolled back.
func (t Table[M, K]) Raw(ctx context.Context, ex database.Executor, sql string, args ...any) ([]M, error) {
	if err := tableError(t.core); err != nil {
		return nil, err
	}
	result := make([]M, 0)
	err := ormcore.Raw(ctx, ex, rawColumns(t.core), sql, args, func(row database.Row) error {
		var item M
		if err := hydrate(t.core, row, unsafe.Pointer(&item)); err != nil {
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
func rawColumns(t *tableCore) []string {
	names := make([]string, len(t.columns))
	for i, c := range t.columns {
		names[i] = c.metadata.Name
	}
	return names
}
