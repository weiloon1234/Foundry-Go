package query

import (
	"context"
	"errors"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

type autocommitQuery func(sqlowner.Seal, context.Context, string, ...any) (*database.Rows, error)

// executeAutocommitInsert sends one INSERT … RETURNING outside an explicit
// transaction when no lifecycle callback can run on a pool: one round trip
// instead of BEGIN, statement and COMMIT. A single-row INSERT is atomic by
// itself. Only an exact pool owner qualifies: a wrapper that embeds one keeps
// its own Transaction, which may establish session state for every write.
// Updates and deletes keep their transaction, because a model declaration does
// not prove a unique key and a multi-row RETURNING result must roll back.
//
// Only a completely read, cleanly closed one-row result confirms the commit.
// Failures before sending report NoCommit, adapter-confirmed rejections
// RolledBack, and any other failure after sending Unknown, retaining a
// hydrated candidate as WriteError exactly like an uncertain transaction.
func executeAutocommitInsert[M any](ctx context.Context, send autocommitQuery, source clock.Clock, plan mutationPlan[M]) (M, error) {
	statement, err := prepareMutation(ctx, &plan, source)
	if err != nil {
		return *new(M), err
	}
	rows, err := send(sqlowner.Seal{}, ctx, statement.sql, statement.arguments...)
	if err != nil {
		return *new(M), err
	}
	var candidate value.Optional[M]
	var readErr error
	for rows.Next() {
		if _, present := candidate.Get(); present {
			readErr = database.NewError("model write returned too many rows", database.TooManyRows)
			break
		}
		model, err := plan.query.definition.scan(rows)
		if err != nil {
			readErr = err
			break
		}
		candidate = value.Set(model)
	}
	err = errors.Join(readErr, rows.Close())
	model, present := candidate.Get()
	if err == nil && present {
		return model, nil
	}
	if err == nil {
		err = database.NewError("model write returned too few rows", database.NotFound)
	}
	err, reconcile := autocommitOutcome(err)
	if present && reconcile {
		return *new(M), &WriteError[M]{cause: err, candidate: model}
	}
	return *new(M), err
}

// autocommitOutcome keeps a statement outcome reported by the database layer.
// A failure without one, or whose graph cannot be inspected completely, was
// observed after sending and becomes Unknown. reconcile reports an outcome
// that may have persisted the candidate.
func autocommitOutcome(err error) (error, bool) {
	var outcome database.Outcome
	inspection := callback.Isolated("inspect autocommit write outcome", func() error {
		found, present, complete := errorgraph.As[*database.Error](err)
		if complete && present && found != nil {
			outcome = found.Outcome()
		}
		return nil
	})
	if inspection != nil {
		err = errors.Join(err, inspection)
		outcome = ""
	}
	switch outcome {
	case database.NoCommit, database.RolledBack:
		return err, false
	case database.Unknown, database.Committed:
		return err, true
	}
	return database.FoundryStatementUnknown(sqlowner.Seal{}, "model write", err), true
}
