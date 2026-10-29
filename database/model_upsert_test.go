package database_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// These batch cases exercise explicit transaction rollback and commit replies.
// Native hook-free InsertMany on an exact pool uses the autocommit fast path.
func TestModelUpsertAndBatchReturningFailures(t *testing.T) {
	name := mutationFields.Name
	policy := orm.OnConflict(orm.ConflictKey(orm.TextExpr(name))).DoUpdate(orm.ConflictIncoming(orm.TextExpr(name)))
	for _, tc := range []struct {
		name  string
		rows  [][]driver.Value
		batch bool
		want  error
	}{
		{"skip", nil, false, nil},
		{"one", [][]driver.Value{{int64(1), "ok"}}, false, nil},
		{"too many", [][]driver.Value{{int64(1), "ok"}, {int64(2), "second"}}, false, database.TooManyRows},
		{"partial decode", [][]driver.Value{{int64(1), "ok"}, {"bad", "second"}}, true, fault.Invalid},
		{"too few", [][]driver.Value{{int64(1), "ok"}}, true, database.NotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := mutationDriver(tc.rows)
			db := open(t, state, nil)
			var err error
			if tc.batch {
				var items []mutationRecord
				items, err = orm.InsertMany(t.Context(), transactionOnly{db}, mutationFields.Table, [][]orm.Assignment[mutationRecord]{mutationValues(), mutationValues()})
				if err != nil && items != nil {
					t.Fatal("partial batch returned")
				}
			} else {
				var item value.Optional[mutationRecord]
				item, err = orm.Upsert(t.Context(), db, mutationModel(), policy, mutationValues()...)
				if item.IsSet() != (tc.want == nil && len(tc.rows) == 1) {
					t.Fatal("optional model lost absence/failure")
				}
			}
			if !errors.Is(err, tc.want) {
				t.Fatal("unexpected write outcome", err)
			}
			if tc.want != nil && (state.committed.Load() != 0 || state.rolledBack.Load() != 1) {
				t.Fatal("failed returning did not roll back")
			}
		})
	}
}

func TestUpsertUnknownCommitRetainsActualResultType(t *testing.T) {
	state := mutationDriver([][]driver.Value{{int64(42), "private candidate"}})
	state.commitError = errors.New("commit response lost")
	db := open(t, state, nil)
	item, err := orm.Upsert(t.Context(), db, mutationModel(), orm.OnConflict[mutationRecord]().DoNothing(), mutationValues()...)
	var typed *orm.WriteError[value.Optional[mutationRecord]]
	if item.IsSet() || !errors.As(err, &typed) || !errors.Is(err, database.CommitUnknown) {
		t.Fatal("optional reconciliation result lost", err)
	}
	candidate, present := typed.Candidate().Get()
	if !present || candidate.ID != 42 {
		t.Fatal("wrong candidate")
	}
	if strings.Contains(fmt.Sprintf("%#v", err), "private") {
		t.Fatal("upsert error disclosed model fields")
	}
}

func TestBatchUnknownCommitAndValidEmptyWrite(t *testing.T) {
	state := mutationDriver([][]driver.Value{{int64(1), "first"}, {int64(2), "second"}})
	state.commitError = errors.New("commit response lost")
	db := open(t, state, nil)
	items, err := orm.InsertMany(t.Context(), transactionOnly{db}, mutationFields.Table, [][]orm.Assignment[mutationRecord]{mutationValues(), mutationValues()})
	var typed *orm.WriteError[[]mutationRecord]
	if items != nil || !errors.As(err, &typed) || len(typed.Candidate()) != 2 {
		t.Fatal("batch reconciliation lost complete models", err)
	}
	state = mutationDriver(nil)
	db = open(t, state, nil)
	connections := state.connected.Load()
	items, err = orm.UpsertMany(t.Context(), db, mutationModel(), orm.OnConflict[mutationRecord]().DoNothing(), nil)
	if err != nil || items == nil || len(items) != 0 || state.connected.Load() != connections || state.committed.Load() != 0 {
		t.Fatal("empty upsert opened a transaction", err)
	}
}

func TestBatchReturningCleanupFailureRollsBack(t *testing.T) {
	cause := errors.New("row cleanup failed")
	state := &driverState{}
	state.query = func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
		return &resultRows{state: state, columns: []string{"id", "name"}, values: [][]driver.Value{{int64(1), "complete"}}, closeError: cause}, nil
	}
	db := open(t, state, nil)
	items, err := orm.InsertMany(t.Context(), transactionOnly{db}, mutationFields.Table, [][]orm.Assignment[mutationRecord]{mutationValues()})
	if !errors.Is(err, cause) || items != nil || state.committed.Load() != 0 || state.rolledBack.Load() != 1 {
		t.Fatal("failed cleanup published or committed models", err)
	}
}
