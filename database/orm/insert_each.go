package orm

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// InsertEach runs each row's ordinary lifecycle in one bounded transaction.
// A nested caller gets one savepoint for the batch, never one per row.
// Example: orm.InsertEach(ctx,db,Users.Table,[][]orm.Assignment[User]{{Users.Name.Set("Ada")}}).
func InsertEach[M any, K comparable](ctx context.Context, writer database.Transactor, table Table[M, K], rows [][]Assignment[M]) ([]M, error) {
	if err := writeContext(ctx, writer); err != nil {
		return nil, err
	}
	if err := tableError(table.core); err != nil {
		return nil, err
	}
	if len(rows) > querycore.MaxPerModelWriteRows {
		return nil, invalid("per-model insert count exceeds its bound")
	}
	plans := make([]writePlan, len(rows))
	for i, row := range rows {
		p, err := planWrite(newQuery(table.core), querycore.InsertModel, row, false)
		if err != nil {
			return nil, err
		}
		plans[i] = p
	}
	if len(rows) == 0 {
		return []M{}, nil
	}
	result := make([]M, 0, len(rows))
	completed := false
	err := writer.Transaction(ctx, func(tx *database.Tx) error {
		if tx == nil {
			return invalid("transactor supplied no transaction")
		}
		for _, p := range plans {
			if err := ctx.Err(); err != nil {
				return err
			}
			inserted, err := mutatePivotPlan(ctx, tx, p, true)
			if err != nil {
				return err
			}
			if len(inserted) != 1 {
				return invalid("per-model insert requires one returned row")
			}
			result = append(result, *(*M)(inserted[0]))
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		completed = true
		return nil
	})
	if err != nil {
		if completed {
			cause, reconcile := inspectOutcome(err, false)
			if reconcile {
				return nil, writeFailure(cause, result)
			}
			return nil, cause
		}
		return nil, err
	}
	if !completed {
		return nil, invalid("transactor returned without a successful batch write")
	}
	return result, nil
}
