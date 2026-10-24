package orm

import (
	"context"
	"errors"
	"slices"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

// CreateOrFirst creates through the model lifecycle in a savepoint, then reads
// the first primary-ordered match only if its own INSERT violates a unique index
// on this table. Failed hooks and uncertain outcomes never trigger recovery or
// retries. At snapshot isolation a competitor may be invisible; the original
// violation is then returned. Internal hydration skips Retrieved callbacks.
// Example: orm.CreateOrFirst(ctx, db, Users.Query().Where(Users.Email.Eq(email)), Users.Name.Set(name)).
func CreateOrFirst[M any](ctx context.Context, writer database.Transactor, q Query[M], create ...Assignment[M]) (M, error) {
	if err := writeContext(ctx, writer); err != nil {
		return *new(M), err
	}
	state := q.state
	state.ast.ScopeContext = ctx
	if state.lock != 0 || state.behavior != 0 || state.relationLimits != nil {
		return *new(M), invalid("lookup writes require an unlocked model query without eager options")
	}
	if _, err := planWrite[M](state, querycore.UpdateModel, nil, false, true); err != nil {
		return *new(M), err
	}
	selected := state
	selected.ast.Orders = []querycore.QueryOrder{{Field: querycore.FieldRef{Table: state.ast.Table, Column: state.table.model.Primary}}}
	selected.ast.Limit = value.Set(1)
	statement, err := compileRead(selected, querycore.ReadModels)
	if err != nil {
		return *new(M), err
	}
	create = slices.Clone(create)
	return atomicModels(ctx, writer, func(tx *database.Tx) (M, error) {
		assignments, err := lookupAssignments(state, create)
		if err != nil {
			return *new(M), err
		}
		insert := newQuery(state.table)
		insert.ast.ScopeContext = ctx
		p, err := planWrite(insert, querycore.InsertModel, assignments, false)
		if err != nil {
			return *new(M), err
		}
		var statementErr ormcore.InsertFailure
		p.statementFailure = &statementErr
		var created M
		// The concrete Tx owns a child savepoint and its after-commit callbacks.
		insertErr, reconcile := executeWrite(ctx, tx, p, func(row database.Row) error { return hydrate(state.table, row, unsafe.Pointer(&created)) }, nil, modelCallbacks(state.table, unsafe.Pointer(&created)))
		if insertErr == nil {
			return requireLookupMatch(ctx, tx, state, created)
		}
		if reconcile {
			return *new(M), &WriteError[M]{cause: insertErr, candidate: created}
		}
		conflict, err := ormcore.OwnInsertConflict(ctx, tx, state.ast.Table, statementErr, insertErr)
		if err != nil {
			return *new(M), errors.Join(insertErr, err)
		}
		if !conflict {
			return *new(M), insertErr
		}
		candidates, _, err := eachCandidates[M](ctx, tx, selected, statement, 1)
		if err != nil {
			return *new(M), errors.Join(insertErr, err)
		}
		if len(candidates) == 0 {
			return *new(M), insertErr
		}
		return candidates[0], nil
	})
}
