package database_test

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

func TestNativeSQLBoundaryDiagnostics(t *testing.T) {
	for _, operation := range []string{"valuer exec", "valuer query", "named valuer", "rows scan", "scan one", "for each scan", "for each yield"} {
		for _, owned := range []bool{false, true} {
			for _, exit := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/owned=%t/goexit=%t", operation, owned, exit), func(t *testing.T) {
					input, raw, cause := databaseDiagnosticInput(t, owned, exit)
					state := &driverState{}
					db := open(t, state, nil)
					var returned error
					var rows *database.Rows
					err := callback.Isolated("consume database result", func() error {
						switch operation {
						case "valuer exec":
							_, returned = db.Exec(t.Context(), "SELECT $1", errorValuer{input})
						case "valuer query":
							_, returned = db.Query(t.Context(), "SELECT $1", errorValuer{input})
						case "named valuer":
							_, returned = db.Exec(t.Context(), "SELECT $1", sql.Named("entry", errorValuer{input}))
						case "rows scan":
							var err error
							rows, err = db.Query(t.Context(), "SELECT 42")
							if err != nil {
								return err
							}
							if !rows.Next() {
								return errors.New("missing diagnostic fixture row")
							}
							returned = rows.Scan(errorScanner{input})
						case "scan one":
							returned = database.ScanOne(t.Context(), db, "SELECT 42", nil, errorScanner{input})
						default:
							returned = database.ForEach(t.Context(), db, "SELECT 42", nil, func(row database.Row) (int64, error) {
								if operation == "for each scan" {
									return 0, input
								}
								var value int64
								err := row.Scan(&value)
								return value, err
							}, func(int64) error { return input })
						}
						return returned
					})
					want := "consume database result failed"
					switch operation {
					case "valuer exec", "named valuer":
						want = "consume database result: database execute: query_failed"
					case "valuer query":
						want = "consume database result: database query: query_failed"
					case "rows scan":
						want = "consume database result: database scan: query_failed"
					case "scan one":
						want = "consume database result: database scan: query_failed\ndatabase scan: query_failed"
					}
					if owned {
						message := ownedDatabaseDiagnostic
						switch operation {
						case "valuer exec", "valuer query", "named valuer":
							message = "convert SQL argument: " + message
						case "rows scan", "scan one":
							message = "scan SQL value: " + message
							if operation == "scan one" {
								message += "\n" + message
							}
						}
						want = "consume database result: " + message
					}
					assertDatabaseDiagnostic(t, err, input, raw, cause, want)
					var classified *database.Error
					if operation == "for each scan" || operation == "for each yield" {
						if returned != input || errors.As(err, &classified) {
							t.Fatal("iterator cleanup changed callback identity or classification")
						}
					} else if !errors.As(err, &classified) || classified.Code() != database.QueryFailed || classified.Outcome() != database.Unspecified {
						t.Fatal("SQL diagnostic changed database classification or outcome")
					}
					if rows != nil && (rows.Err() != returned || rows.Close() != returned) {
						t.Fatal("row inspection or repeated close lost the original scan result")
					}
					wantClosed := int64(1)
					if operation == "valuer exec" || operation == "valuer query" || operation == "named valuer" {
						wantClosed = 0
					}
					if state.rowsClosed.Load() != wantClosed || db.Stats().Owners != 0 || db.Stats().InUse != 0 {
						t.Fatal("SQL failure retained rows or connection ownership")
					}
					if _, err := db.Exec(t.Context(), "SELECT 1"); err != nil {
						t.Fatal("SQL callback failure prevented connection reuse")
					}
				})
			}
		}
	}
}
