package ormcore

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/internal/safeerror"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// InsertFailure binds statement provenance to the child savepoint that executed it.
// Recovery requires that concrete child to have confirmed rollback.
type InsertFailure struct {
	cause error
	scope *database.Tx
}

// InsertRows records only errors from the model's own statement, never hydration
// or lifecycle errors. A nil capture leaves ordinary writes unchanged.
func InsertRows(ctx context.Context, tx *database.Tx, statement querycore.Statement, capture *InsertFailure) (RowSource, error) {
	if capture != nil {
		capture.scope = tx
	}
	rows, err := tx.Query(statement.Context(ctx), statement.SQLText, statement.Bindings...)
	if capture == nil {
		return rows, err
	}
	if err != nil {
		capture.cause = err
		return nil, err
	}
	return insertRows{Rows: rows, failure: capture}, nil
}

type insertRows struct {
	*database.Rows
	failure *InsertFailure
}

func (r insertRows) Err() error {
	err := r.Rows.Err()
	r.failure.cause = safeerror.Join("operation failed", r.failure.cause, err)
	return err
}
func (r insertRows) Close() error {
	err := r.Rows.Close()
	r.failure.cause = safeerror.Join("operation failed", r.failure.cause, err)
	return err
}

// OwnInsertConflict resolves only a known statement rejection on this table.
// Incomplete error inspection and uncertain transaction outcomes never authorize
// competitor retrieval. The concrete child must confirm savepoint rollback.
func OwnInsertConflict(ctx context.Context, tx *database.Tx, table string, failure InsertFailure, insertErr error) (bool, error) {
	if failure.cause == nil || insertErr == nil || failure.scope == nil || failure.scope.State() != database.TxRolledBack {
		return false, nil
	}
	var constraint, schema, relation string
	safe := true
	err := callback.Isolated("classify lookup insert failure", func() error {
		complete := errorgraph.Walk(insertErr, func(current error) bool {
			if failure, ok := errorgraph.AsShallow[*database.Error](current); ok {
				if failure == nil || failure.Outcome() == database.Unknown || failure.Outcome() == database.Committed || failure.Code() == database.CommitUnknown {
					safe = false
				}
			}
			return true
		})
		if !complete || !safe {
			safe = false
			return nil
		}
		failure, found, complete := errorgraph.As[*database.Error](failure.cause)
		if complete && found && failure != nil && failure.Code() == database.UniqueViolation {
			constraint, schema, relation = failure.Constraint(), failure.Schema(), failure.Table()
		}
		return nil
	})
	if err != nil || !safe || constraint == "" || schema == "" || relation == "" {
		return false, err
	}
	var owned bool
	err = database.ScanOne(ctx, tx, `SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_index i JOIN pg_catalog.pg_class c ON c.oid = i.indexrelid JOIN pg_catalog.pg_class t ON t.oid = i.indrelid JOIN pg_catalog.pg_namespace n ON n.oid = t.relnamespace WHERE i.indisunique AND i.indrelid = to_regclass($1) AND c.relname = $2 AND n.nspname = $3 AND t.relname = $4)`, []any{querycore.QuotedTable(table), constraint, schema, relation}, &owned)
	return owned, err
}
