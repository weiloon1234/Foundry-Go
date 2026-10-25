package orm

import (
	"context"
	"database/sql/driver"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
	"slices"
)

// PruneBatch physically removes at most size ordinary models in primary-key order.
// Mass explicitly skips per-model callbacks; otherwise ordinary deletion lifecycle runs.
// Soft-delete declarations require ForcePruneBatch and their declaration token.
// Example: orm.PruneBatch(ctx, db, Logs.Query().Where(Logs.Expired.Eq(true)), 100, false).
func PruneBatch[M any](ctx context.Context, writer database.Transactor, q Query[M], size int, mass bool) (int64, error) {
	if q.state.ast.HasSoftDeletes() {
		return 0, invalid("soft-model pruning requires ForcePruneBatch and its SoftDeletable token")
	}
	return pruneBatch[M](ctx, writer, q.state, size, mass, querycore.DeleteModel)
}

// ForcePruneBatch physically removes a bounded soft-model batch within its visibility.
// Mass acknowledges skipping hooks, observers and change capture; selection still locks rows.
// Example: orm.ForcePruneBatch(ctx, db, Users.Query().OnlyTrashed(), Users.Soft, 100, false).
func ForcePruneBatch[M any](ctx context.Context, writer database.Transactor, q Query[M], soft SoftDeletable[M], size int, mass bool) (int64, error) {
	if err := checkSoftDelete(soft.capability, q.state.table); err != nil {
		return 0, err
	}
	return pruneBatch[M](ctx, writer, q.state, size, mass, querycore.ForceDeleteModel)
}
func pruneBatch[M any](ctx context.Context, writer database.Transactor, q queryState, size int, mass bool, kind querycore.MutationKind) (int64, error) {
	if err := writeContext(ctx, writer); err != nil {
		return 0, err
	}
	q.ast.ScopeContext = ctx
	if mass {
		q.ast.SkipModelHooks = false
	}
	p, _, err := planEach(q, kind, size)
	if err != nil {
		return 0, err
	}
	selected := p.query
	selected.ast.Orders = []querycore.QueryOrder{{Field: querycore.FieldRef{Table: q.ast.Table, Column: q.table.model.Primary}}}
	selected.ast.Limit = value.Set(size)
	selected.lock = querycore.LockUpdate
	statement, err := compileRead(selected, querycore.ReadModels)
	if err != nil {
		return 0, err
	}
	return atomicModels(ctx, writer, func(tx *database.Tx) (int64, error) {
		_, keys, err := eachCandidates[M](ctx, tx, selected, statement, size)
		if err != nil || len(keys) == 0 {
			return 0, err
		}
		values := make([]any, len(keys))
		for i, key := range keys {
			values[i] = key
		}
		selected = p.query
		selected.ast.Predicates = append(slices.Clone(selected.ast.Predicates), querycore.Comparison{Operand: querycore.FieldRef{Table: q.ast.Table, Column: q.table.model.Primary}, Operator: querycore.In, Values: querycore.RetainComparisonValues(values), Kind: q.table.primary.kind, Bind: func(v any) (driver.Value, error) {
			a, err := q.table.primary.automatic(v)
			if err != nil {
				return nil, err
			}
			return a.Bind()
		}})
		if !mass {
			removed, err := writeEach[M](ctx, eachTransaction{tx}, selected, kind, size, nil)
			return int64(len(removed)), err
		}
		plan, err := planWrite[M](selected, kind, nil, true)
		if err != nil {
			return 0, err
		}
		var count int64
		err, _ = executeWrite(ctx, eachTransaction{tx}, plan, nil, &count, nil)
		return count, err
	})
}
