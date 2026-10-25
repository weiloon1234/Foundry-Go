package orm

import (
	"context"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Both the lifecycle locking read and UPDATE classify a clean empty result here.
// Row/close failures must propagate without attempting another statement.
func (p writePlan) returningAdjustment(ctx context.Context, tx *database.Tx, rows rowSource, scan func(database.Row) error) error {
	if !p.bounded {
		return returning(ctx, rows, scan)
	}
	n, err := returningCount(ctx, rows, scan, 1)
	if err != nil || n != 0 {
		return err
	}
	q := p.query.ast
	q.ScopeContext = ctx
	// Preserve every caller predicate, including predicates appended after planning
	// (such as a concrete pivot identity). Remove only this assignment's guard.
	q.Predicates = slices.Delete(slices.Clone(q.Predicates), p.boundIndex, p.boundIndex+1)
	q.Limit = value.Set(1)
	node := q.ModelSelect([]querycore.SelectItem{{Expression: querycore.FieldRef{Table: q.Table, Column: q.Model.Primary}}})
	node.Locks = []querycore.LockSpec{{Strength: querycore.LockUpdate, Behavior: p.query.behavior}}
	statement, err := querycore.CompileRead(node, ctx, true, nil, true)
	if err != nil {
		return err
	}
	check, err := tx.Query(statement.Context(ctx), statement.SQLText, statement.Bindings...)
	if err != nil {
		return err
	}
	n, err = returningCount(ctx, check, func(database.Row) error { return nil }, 1)
	if err != nil {
		return err
	}
	if n == 0 {
		return database.NewError("model write returned too few rows", database.NotFound)
	}
	return boundExceeded()
}
