package database_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type mutationRecord struct {
	ID   int64
	Name string
}

func mutationModel() query.Query[mutationRecord] {
	return query.ForModel(mutationDefinition())
}

func mutationDefinition() query.Definition[mutationRecord] {
	return query.Define("records", "id", []query.Column{{Name: "id", DatabaseDefault: true}, {Name: "name"}}, func(row database.Row) (mutationRecord, error) {
		var record mutationRecord
		if err := row.Scan(codec.Signed[int64]().Scan(&record.ID), codec.String[string]().Scan(&record.Name)); err != nil {
			return mutationRecord{}, err
		}
		return record, nil
	}, query.NewModelField("id", codec.Signed[int64](), func(m mutationRecord) int64 { return m.ID }), query.NewModelField("name", codec.String[string](), func(m mutationRecord) string { return m.Name }))
}

func mutationValues() query.Mutation[mutationRecord] {
	return query.Change(query.Assign[mutationRecord]("records", "name", codec.String[string](), "private record name"))
}

func mutationDriver(values [][]driver.Value) *driverState {
	state := &driverState{}
	state.query = func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
		return &resultRows{state: state, columns: []string{"id", "name"}, values: values}, nil
	}
	return state
}

func TestModelMutationReturnsOnlyOneHydratedModel(t *testing.T) {
	for _, test := range []struct {
		name     string
		rows     [][]driver.Value
		expected error
	}{
		{"one", [][]driver.Value{{int64(42), "private record name"}}, nil},
		{"missing", nil, database.NotFound},
		{"multiple", [][]driver.Value{{int64(42), "first"}, {int64(43), "second"}}, database.TooManyRows},
		{"invalid hydration", [][]driver.Value{{"overflow", "partial"}}, fault.Invalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := mutationDriver(test.rows)
			db := open(t, state, nil)
			model, err := mutationModel().Insert(t.Context(), db, mutationValues())
			if test.expected == nil {
				if err != nil || model.ID != 42 || state.committed.Load() != 1 {
					t.Fatalf("write failed: %v", err)
				}
			} else if !errors.Is(err, test.expected) || model != (mutationRecord{}) || state.committed.Load() != 0 || state.rolledBack.Load() != 1 {
				t.Fatalf("write failure leaked state: %v", err)
			}
		})
	}
}

func TestModelMutationUnknownCommitRetainsTypedReconciliationCandidate(t *testing.T) {
	state := mutationDriver([][]driver.Value{{int64(42), "private record name"}})
	state.commitError = errors.New("commit response lost")
	db := open(t, state, nil)
	model, err := mutationModel().Insert(t.Context(), db, mutationValues())
	var writeError *query.WriteError[mutationRecord]
	var databaseError *database.Error
	if model != (mutationRecord{}) || !errors.Is(err, database.CommitUnknown) || !errors.As(err, &writeError) || !errors.As(err, &databaseError) || databaseError.Outcome() != database.Unknown || writeError.Candidate().ID != 42 {
		t.Fatalf("uncertain commit lost reconciliation information: %v", err)
	}
	for _, format := range []string{"%v", "%+v", "%#v"} {
		if strings.Contains(fmt.Sprintf(format, err), "private record name") {
			t.Fatal("write error exposed model fields")
		}
	}
	if state.connected.Load() != 1 || state.committed.Load() != 1 || state.closed.Load() != 1 {
		t.Fatal("uncertain write retried or retained its physical connection")
	}
}

func TestModelMutationNestedScopeAndValidationBeforeTransaction(t *testing.T) {
	state := mutationDriver([][]driver.Value{{int64(42), "private record name"}})
	var begins atomic.Int64
	state.begin = func(context.Context, driver.TxOptions) (driver.Tx, error) {
		begins.Add(1)
		return transaction{state}, nil
	}
	db := open(t, state, nil)
	if _, err := mutationModel().Insert(t.Context(), db, query.Change[mutationRecord]()); !errors.Is(err, fault.Missing) || begins.Load() != 0 {
		t.Fatal("missing field bypassed validation")
	}
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		if err := tx.Transaction(t.Context(), func(*database.Tx) error { return nil }, database.TxOptions{}); !errors.Is(err, fault.Invalid) {
			return errors.New("nested isolation options were accepted")
		}
		if _, err := mutationModel().Insert(t.Context(), tx, mutationValues()); err != nil {
			return err
		}
		if state.committed.Load() != 0 {
			return errors.New("nested model write committed its outer transaction")
		}
		return nil
	})
	if err != nil || state.committed.Load() != 1 || begins.Load() != 1 {
		t.Fatalf("nested model scope failed: %v", err)
	}
}

type afterCommitTransactor struct {
	db    *database.DB
	cause error
}

func (w afterCommitTransactor) Transaction(ctx context.Context, fn func(*database.Tx) error, options ...database.TxOptions) error {
	return w.db.Transaction(ctx, func(tx *database.Tx) error {
		if err := fn(tx); err != nil {
			return err
		}
		return tx.AfterCommit(func(context.Context) error { return w.cause })
	}, options...)
}

func TestModelMutationAfterCommitFailurePreservesCommittedCandidate(t *testing.T) {
	state := mutationDriver([][]driver.Value{{int64(42), "private record name"}})
	db := open(t, state, nil)
	cause := errors.New("notification failed")
	model, err := mutationModel().Insert(t.Context(), afterCommitTransactor{db, cause}, mutationValues())
	var writeError *query.WriteError[mutationRecord]
	var databaseError *database.Error
	if model != (mutationRecord{}) || !errors.Is(err, database.AfterCommitFailed) || !errors.Is(err, cause) || !errors.As(err, &writeError) || !errors.As(err, &databaseError) || databaseError.Outcome() != database.Committed || writeError.Candidate().ID != 42 {
		t.Fatalf("committed write lost its outcome or reconciliation data: %v", err)
	}
	if state.committed.Load() != 1 || state.rolledBack.Load() != 0 {
		t.Fatal("after-commit failure retried or rolled back a committed write")
	}
}
