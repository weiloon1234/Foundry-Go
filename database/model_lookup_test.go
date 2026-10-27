package database_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func TestInsertOrFirstRejectsIncompleteConflictIdentity(t *testing.T) {
	for _, missing := range []string{"schema", "table", "constraint"} {
		t.Run(missing, func(t *testing.T) {
			cause := errors.New("insert rejected")
			detail := database.Detail{Code: database.UniqueViolation, SQLState: "23505", Schema: "public", Table: "records", Constraint: "records_name_key"}
			switch missing {
			case "schema":
				detail.Schema = ""
			case "table":
				detail.Table = ""
			case "constraint":
				detail.Constraint = ""
			}
			state := &driverState{}
			inserts, reads := 0, 0
			state.query = func(_ context.Context, statement string, _ []driver.NamedValue) (driver.Rows, error) {
				if strings.HasPrefix(statement, "INSERT INTO") {
					inserts++
					return nil, cause
				}
				reads++
				return nil, errors.New("unexpected conflict recovery read")
			}
			db, err := database.Open(t.Context(), database.Adapter{Connector: connector{state}, Classify: func(err error) database.Detail {
				if errors.Is(err, cause) {
					return detail
				}
				return database.Detail{Code: database.QueryFailed}
			}}, database.DefaultPoolConfig())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := db.Close(context.Background()); err != nil {
					t.Error(err)
				}
			})
			name := mutationFields.Name
			got, err := orm.CreateOrFirst(t.Context(), db, mutationModel().Where(name.Eq("private record name")), mutationValues()...)
			var failure *database.Error
			if got != (mutationRecord{}) || !errors.Is(err, cause) || !errors.Is(err, database.UniqueViolation) || !errors.As(err, &failure) || failure.Outcome() != database.RolledBack {
				t.Fatal("incomplete identity lost the original insert failure", err)
			}
			if inserts != 1 || reads != 0 || state.committed.Load() != 0 || state.rolledBack.Load() != 1 || db.Stats().Owners != 0 {
				t.Fatal("incomplete identity triggered recovery, retry or leaked ownership", inserts, reads)
			}
		})
	}
}

func TestInsertOrFirstUnknownCommitRetainsReconciliationIdentityWithoutRetry(t *testing.T) {
	cause := errors.New("commit response lost")
	state := &driverState{commitError: cause}
	begins, inserts, matches, recoveryReads := 0, 0, 0, 0
	state.begin = func(context.Context, driver.TxOptions) (driver.Tx, error) {
		begins++
		return transaction{state}, nil
	}
	state.query = func(_ context.Context, statement string, _ []driver.NamedValue) (driver.Rows, error) {
		switch {
		case strings.HasPrefix(statement, "INSERT INTO"):
			inserts++
			return &resultRows{state: state, columns: []string{"id", "name"}, values: [][]driver.Value{{int64(42), "private record name"}}}, nil
		case strings.HasPrefix(statement, "SELECT EXISTS") && !strings.Contains(statement, "pg_catalog"):
			matches++
			return &resultRows{state: state, values: [][]driver.Value{{true}}}, nil
		default:
			recoveryReads++
			return nil, errors.New("unexpected conflict recovery read")
		}
	}
	db := open(t, state, nil)
	name := mutationFields.Name
	got, err := orm.CreateOrFirst(t.Context(), db, mutationModel().Where(name.Eq("private record name")), mutationValues()...)
	var writeError *orm.WriteError[mutationRecord]
	var failure *database.Error
	if got != (mutationRecord{}) || !errors.Is(err, cause) || !errors.Is(err, database.CommitUnknown) || !errors.As(err, &failure) || failure.Outcome() != database.Unknown || !errors.As(err, &writeError) {
		t.Fatal("unknown commit returned success or lost reconciliation information", err)
	}
	if writeError.Candidate() != (mutationRecord{ID: 42, Name: "private record name"}) {
		t.Fatal("unknown commit lost the inserted model identity")
	}
	if begins != 1 || inserts != 1 || matches != 1 || recoveryReads != 0 || state.committed.Load() != 1 || state.rolledBack.Load() != 0 || state.closed.Load() != 1 || db.Stats().Owners != 0 {
		t.Fatal("unknown commit triggered recovery, retry or retained its connection", begins, inserts, matches, recoveryReads)
	}
}
