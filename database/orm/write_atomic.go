package orm

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/lifecore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"slices"
	"unsafe"
)

// atomicModels keeps candidate publication and outcome classification at the transaction boundary.
func atomicModels[R any](ctx context.Context, writer database.Transactor, run func(*database.Tx) (R, error)) (R, error) {
	if nilInterface(writer) {
		return *new(R), invalid("nil transactor")
	}
	var result R
	completed := false
	err := writer.Transaction(ctx, func(tx *database.Tx) error {
		if tx == nil {
			return invalid("transactor supplied no transaction")
		}
		return callback.Isolated("atomic model operation", func() error {
			var err error
			result, err = run(tx)
			if err != nil {
				return lifecore.Failure("atomic model operation failed", err)
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			completed = true
			return nil
		})
	})
	if err != nil {
		if completed {
			classified, reconcile := inspectOutcome(err, false)
			if reconcile {
				return *new(R), writeFailure(classified, result)
			}
			err = classified
		}
		return *new(R), err
	}
	if !completed {
		return *new(R), invalid("transactor returned without a successful model operation")
	}
	return result, nil
}
func requireLookupMatch[M any](ctx context.Context, tx *database.Tx, q queryState, created M) (M, error) {
	key, err := q.table.primary.get(unsafe.Pointer(&created))
	if err != nil {
		return *new(M), err
	}
	q.ast.Predicates = append(slices.Clone(q.ast.Predicates), eachPrimary(q, key))
	var matches bool
	if err := scalarRead(ctx, tx, q, querycore.ReadExists, &matches); err != nil {
		return *new(M), err
	}
	if !matches {
		return *new(M), invalid("created model does not satisfy lookup predicates and visibility")
	}
	return created, nil
}
