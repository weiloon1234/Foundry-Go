package orm

import (
	"context"
	"database/sql/driver"
	"slices"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/lifecore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlvalue"
	"github.com/weiloon1234/Foundry-Go/value"
)

// MaxPerModelWriteRows bounds one atomic lifecycle batch. Example: limit <= orm.MaxPerModelWriteRows.
const MaxPerModelWriteRows = querycore.MaxPerModelWriteRows

// UpdateEach locks the complete candidate set in primary-key order, then derives
// typed assignments and runs each ordinary model lifecycle in one transaction.
// Read ordering, pagination, eager loading and WithoutModelHooks are rejected.
// The callback gets the actual transaction; propagate its context to nested work.
// Example: orm.UpdateEach(ctx,db,Users.Query(),100,func(ctx context.Context,tx *database.Tx,u User)([]orm.Assignment[User],error){return []orm.Assignment[User]{orm.TextSet(Users.Name, u.Name)},nil}).
func UpdateEach[M any](ctx context.Context, writer database.Transactor, q Query[M], limit int, draft func(context.Context, *database.Tx, M) ([]Assignment[M], error)) ([]M, error) {
	if draft == nil {
		return nil, invalid("per-model update requires a draft callback")
	}
	return writeEach(ctx, writer, q.state, querycore.UpdateModel, limit, draft)
}

// DeleteEach removes each active candidate through ordinary delete/soft-delete
// lifecycle callbacks. Overflow fails before callbacks, and later failures roll
// back the complete batch and its after-commit work. Results follow primary keys.
// Example: orm.DeleteEach(ctx,db,Notifications.Query().Where(orm.ColumnEq(Notifications.Read, true)),100).
func DeleteEach[M any](ctx context.Context, writer database.Transactor, q Query[M], limit int) ([]M, error) {
	return writeEach[M](ctx, writer, q.state, querycore.DeleteModel, limit, nil)
}

// RestoreEach restores the selected deleted models in one bounded transaction.
// It retains predicates, selects only trashed rows, and runs each restore lifecycle.
// Example: orm.RestoreEach(ctx, db, Users.Query(), Users.Soft, 100).
func RestoreEach[M any](ctx context.Context, writer database.Transactor, q Query[M], soft SoftDeletable[M], limit int) ([]M, error) {
	if err := checkSoftDelete(soft.capability, q.state.table); err != nil {
		return nil, err
	}
	return writeEach[M](ctx, writer, q.state, querycore.RestoreModel, limit, nil)
}

// ForceDeleteEach physically deletes selected soft-delete models in one bounded
// transaction, preserving the query's visibility and running each force-delete
// lifecycle. Returned models describe their persisted state before deletion.
// Example: orm.ForceDeleteEach(ctx, db, Users.Query().OnlyTrashed(), Users.Soft, 100).
func ForceDeleteEach[M any](ctx context.Context, writer database.Transactor, q Query[M], soft SoftDeletable[M], limit int) ([]M, error) {
	if err := checkSoftDelete(soft.capability, q.state.table); err != nil {
		return nil, err
	}
	return writeEach[M](ctx, writer, q.state, querycore.ForceDeleteModel, limit, nil)
}

// eachTransaction is private and only used inside the batch transaction. It
// avoids a savepoint per row; any row failure aborts the enclosing atomic batch.
// It carries no capabilities and does not call application-provided interfaces.
type eachTransaction struct{ tx *database.Tx }

func (e eachTransaction) Transaction(ctx context.Context, fn func(*database.Tx) error, options ...database.TxOptions) error {
	if len(options) != 0 {
		return invalid("nested model writes cannot set transaction options")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn(e.tx)
}

func planEach(q queryState, kind querycore.MutationKind, limit int) (writePlan, querycore.Statement, error) {
	if q.lock != 0 || q.behavior != 0 {
		return writePlan{}, querycore.Statement{}, invalid("per-model writes require an unlocked query")
	}
	if limit < 1 || limit > MaxPerModelWriteRows {
		return writePlan{}, querycore.Statement{}, invalid("invalid per-model write row limit")
	}
	if q.ast.SkipModelHooks {
		return writePlan{}, querycore.Statement{}, invalid("per-model writes require model hooks")
	}
	if q.relationLimits != nil {
		return writePlan{}, querycore.Statement{}, invalid("model writes cannot include eager-loading options")
	}
	// Reuse write-shape validation without requiring a primary equality until the
	// candidate key is attached. This also owns soft-delete scope normalization.
	p, err := planWrite[struct{}](q, kind, nil, true)
	if err != nil {
		return writePlan{}, querycore.Statement{}, err
	}
	if p.query.table.primary == nil || p.query.table.primary.get == nil {
		return writePlan{}, querycore.Statement{}, invalid("per-model write requires a primary-key codec")
	}
	selected := p.query
	selected.ast.Orders = []querycore.QueryOrder{{Field: querycore.FieldRef{Table: selected.ast.Table, Column: selected.table.model.Primary}}}
	selected.ast.Limit = value.Set(limit + 1)
	selected.lock = querycore.LockUpdate
	statement, err := compileRead(selected, querycore.ReadModels)
	return p, statement, err
}

func writeEach[M any](ctx context.Context, writer database.Transactor, q queryState, kind querycore.MutationKind, limit int, draft func(context.Context, *database.Tx, M) ([]Assignment[M], error)) ([]M, error) {
	if err := writeContext(ctx, writer); err != nil {
		return nil, err
	}
	if nilInterface(writer) {
		return nil, invalid("nil transactor")
	}
	q.ast.ScopeContext = ctx
	p, statement, err := planEach(q, kind, limit)
	if err != nil {
		return nil, err
	}
	var result []M
	completed := false
	err = writer.Transaction(ctx, func(tx *database.Tx) error {
		if tx == nil {
			return invalid("transactor supplied no transaction")
		}
		candidates, keys, err := eachCandidates[M](ctx, tx, p.query, statement, limit)
		if err != nil {
			return err
		}
		result = make([]M, 0, len(candidates))
		for i, candidate := range candidates {
			if err := ctx.Err(); err != nil {
				return err
			}
			var assignments []Assignment[M]
			if draft != nil {
				work, err := lifecore.Context(ctx)
				if err != nil {
					return err
				}
				// Isolate panic/Goexit while retaining the transaction until callback exit.
				err = callback.Isolated("per-model update draft", func() error {
					var err error
					assignments, err = draft(work, tx, candidate)
					return lifecore.Failure("per-model update draft failed", err)
				})
				if err != nil {
					return err
				}
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			selected := p.query
			selected.ast.Predicates = append(slices.Clone(selected.ast.Predicates), eachPrimary(selected, keys[i]))
			changed, err := eachMutation(ctx, tx, selected, p.kind, assignments)
			if err != nil {
				return err
			}
			result = append(result, changed)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		completed = true
		return nil
	})
	if err != nil {
		if completed {
			var reconcile bool
			err, reconcile = inspectOutcome(err, false)
			if reconcile {
				return nil, writeFailure(err, result)
			}
		}
		return nil, err
	}
	if !completed {
		return nil, invalid("transactor returned without a successful model write")
	}
	return result, nil
}

func eachCandidates[M any](ctx context.Context, tx *database.Tx, q queryState, statement querycore.Statement, limit int) ([]M, []driver.Value, error) {
	rows, err := tx.Query(statement.Context(ctx), statement.SQLText, statement.Bindings...)
	if err != nil {
		return nil, nil, err
	}
	candidates := make([]M, 0)
	err = consume(ctx, rows, func(row database.Row) error {
		if len(candidates) >= limit+1 {
			return querycore.DatabaseFailure("model write returned too many rows", database.TooManyRows)
		}
		var item M
		if err := hydrate(q.table, row, unsafe.Pointer(&item)); err != nil {
			return err
		}
		candidates = append(candidates, item)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if len(candidates) > limit {
		return nil, nil, invalid("per-model write exceeds its row limit")
	}
	keys := make([]driver.Value, len(candidates))
	seen := make(map[sqlvalue.Value]bool, len(candidates))
	for i := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		key, err := q.table.primary.get(unsafe.Pointer(&candidates[i]))
		if err != nil {
			return nil, nil, err
		}
		if key == nil {
			return nil, nil, invalid("per-model write primary key cannot be NULL")
		}
		identity, err := querycore.RecordEqualityKey(q.table.primary.kind, key)
		if err != nil {
			return nil, nil, err
		}
		if seen[identity] {
			return nil, nil, querycore.DatabaseFailure("per-model write identity", database.TooManyRows)
		}
		seen[identity] = true
		if bytes, ok := key.([]byte); ok {
			key = slices.Clone(bytes)
		}
		keys[i] = key
	}
	return candidates, keys, nil
}
func eachPrimary(q queryState, key driver.Value) querycore.Expression {
	return querycore.Comparison{Operand: querycore.FieldRef{Table: q.ast.Table, Column: q.table.model.Primary}, Operator: querycore.Equal, Values: querycore.RetainComparisonValues([]any{key}), Bind: func(v any) (driver.Value, error) { return v, nil }}
}

func eachMutation[M any](ctx context.Context, tx *database.Tx, q queryState, kind querycore.MutationKind, assignments []Assignment[M]) (M, error) {
	p, err := planWrite(q, kind, assignments, false)
	if err != nil {
		return *new(M), err
	}
	var changed M
	callbacks := modelCallbacks(q.table, unsafe.Pointer(&changed))
	// The concrete transaction supplies the trusted observer snapshot; the private
	// wrapper need not expose any seal or implement observer lookup interfaces.
	if !callbacks.needed(tx.Observers(), p.kind) {
		callbacks = nil
	}
	err, _ = executeWrite(ctx, eachTransaction{tx}, p, func(row database.Row) error { return hydrate(q.table, row, unsafe.Pointer(&changed)) }, nil, callbacks)
	if err != nil {
		return *new(M), err
	}
	return changed, nil
}
