package database_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

func TestClassifiedDatabaseDiagnosticsCrossCallbacks(t *testing.T) {
	for _, test := range []struct {
		name   string
		code   database.Code
		native error
		want   database.Code
	}{
		{"default", "", nil, database.QueryFailed},
		{"known adapter", database.UniqueViolation, nil, database.UniqueViolation},
		{"private adapter", database.Code("private adapter classification"), nil, database.Code("private adapter classification")},
		{"known copied", database.UniqueViolation, nil, database.UniqueViolation},
		{"private copied", database.Code("private copied classification"), nil, database.Code("private copied classification")},
		{"native cancellation", database.Code("private adapter classification"), context.Canceled, database.Canceled},
		{"native missing", database.Code("private adapter classification"), sql.ErrNoRows, database.NotFound},
	} {
		for _, exit := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/goexit=%t", test.name, exit), func(t *testing.T) {
				cause := &privateDatabaseDiagnostic{exit: exit}
				input := errors.Join(test.native, cause)
				if test.name == "known copied" || test.name == "private copied" {
					input = errors.Join(database.NewError("private copied operation", test.code), cause)
				}
				state := &driverState{exec: func(context.Context, string, []driver.NamedValue) (driver.Result, error) { return nil, input }}
				db, err := database.Open(t.Context(), database.Adapter{Connector: connector{state}, Classify: func(error) database.Detail { return database.Detail{Code: test.code} }}, database.DefaultPoolConfig())
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := db.Close(context.Background()); err != nil {
						t.Error(err)
					}
				})
				var result error
				err = callback.Isolated("consumer", func() error { _, result = db.Exec(t.Context(), "private SQL"); return result })
				want := "consumer: database execute: " + string(test.want)
				if test.name == "private adapter" || test.name == "private copied" {
					want = "consumer failed"
				}
				var detail *database.Error
				var retained *privateDatabaseDiagnostic
				if err == nil || err.Error() != want || !errors.Is(err, result) || !errors.Is(err, input) || !errors.Is(err, cause) || !errors.Is(err, test.want) || !errors.As(err, &detail) || detail.Outcome() != database.Unspecified || !errors.As(err, &retained) || retained != cause {
					t.Fatal("database classification lost diagnostic, identity or outcome")
				}
				*detail = *database.NewError("private replacement operation", database.Code("private replacement code")).(*database.Error)
				if fmt.Sprintf("%v %+v %#v", err, err, err) != want+" "+want+" "+want || cause.formatted.Load() != 0 || db.Stats().Owners != 0 || db.Stats().InUse != 0 {
					t.Fatal("database diagnostic formatted private text or retained ownership")
				}
			})
		}
	}
}

func TestDatabaseFinalOutcomeDiagnostic(t *testing.T) {
	for _, operation := range []string{"unknown commit", "rejected commit", "rolled back statement", "unknown statement", "sealed statement", "after commit"} {
		for _, exit := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/goexit=%t", operation, exit), func(t *testing.T) {
				cause := &privateDatabaseDiagnostic{exit: exit}
				state := &driverState{}
				if operation == "unknown commit" || operation == "rejected commit" {
					state.commitError = cause
				}
				if operation == "rolled back statement" {
					state.exec = func(context.Context, string, []driver.NamedValue) (driver.Result, error) { return nil, cause }
				}
				if operation == "unknown statement" {
					state.query = func(context.Context, string, []driver.NamedValue) (driver.Rows, error) { return nil, cause }
				}
				db, err := database.Open(t.Context(), database.Adapter{Connector: connector{state}, Classify: func(error) database.Detail {
					return database.Detail{Code: database.QueryFailed, CommitRejected: operation == "rejected commit"}
				}}, database.DefaultPoolConfig())
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := db.Close(context.Background()); err != nil {
						t.Error(err)
					}
				})
				var result error
				err = callback.Isolated("consumer", func() error {
					switch operation {
					case "unknown statement":
						_, result = db.FoundryAutocommitQuery(sqlowner.Autocommit(), t.Context(), "private SQL")
					case "sealed statement":
						result = database.FoundryStatementUnknown(sqlowner.Outcomes(), "model write", cause)
					default:
						result = db.Transaction(t.Context(), func(tx *database.Tx) error {
							if operation == "rolled back statement" {
								_, err := tx.Exec(t.Context(), "private SQL")
								return err
							}
							if operation == "after commit" {
								return tx.AfterCommit(func(context.Context) error { return cause })
							}
							return nil
						})
					}
					return result
				})
				code, outcome, want := database.CommitUnknown, database.Unknown, "consumer: database commit: commit_unknown"
				switch operation {
				case "rolled back statement":
					code, outcome, want = database.QueryFailed, database.RolledBack, "consumer: database execute: query_failed"
				case "rejected commit":
					code, outcome, want = database.QueryFailed, database.RolledBack, "consumer: database commit: query_failed"
				case "unknown statement":
					want = "consumer: database query: commit_unknown"
				case "sealed statement":
					want = "consumer: database model write: commit_unknown"
				case "after commit":
					code, outcome, want = database.AfterCommitFailed, database.Committed, "consumer: after commit: database after commit: after_commit_failed"
				}
				var detail *database.Error
				var retained *privateDatabaseDiagnostic
				if err == nil || err.Error() != want || !errors.Is(err, result) || !errors.Is(err, cause) || !errors.Is(err, code) || !errors.As(err, &detail) || detail.Code() != code || detail.Outcome() != outcome || !errors.As(err, &retained) || retained != cause {
					t.Fatal("final diagnostic changed classification, outcome or identity")
				}
				*detail = *database.NewError("private replacement", database.Code("private replacement")).(*database.Error)
				if fmt.Sprintf("%v %+v %#v", err, err, err) != want+" "+want+" "+want || cause.formatted.Load() != 0 || db.Stats().Owners != 0 || db.Stats().InUse != 0 {
					t.Fatal("final diagnostic leaked private text or retained ownership")
				}
			})
		}
	}
}

func TestPublicDatabaseErrorDoesNotGrantDiagnosticOwnership(t *testing.T) {
	original := database.NewError("private application operation", database.QueryFailed)
	err := callback.Isolated("consumer", func() error { return original })
	if err.Error() != "consumer failed" || !errors.Is(err, original) || !errors.Is(err, database.QueryFailed) {
		t.Fatal("public database factory granted diagnostic ownership")
	}
}
