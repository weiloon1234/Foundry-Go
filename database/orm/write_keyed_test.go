package orm

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type keyedRow struct {
	ID    int64
	Scope string
	Name  string
}
type keyedTable struct {
	Table[keyedRow, int64]
	Scope Text[keyedRow, string]
	Name  Text[keyedRow, string]
}

func keyedFields(options ...Option) keyedTable {
	return Define("keyed", func(b Builder[keyedRow], m *keyedRow) keyedTable {
		return keyedTable{TableOf(b, &m.ID), TextOf(b, &m.Scope), TextOf(b, &m.Name)}
	}, options...)
}

// Replay the captured generated Update/Delete expansion: scoped primary equality,
// Patch/Remove, lifecycle trace and caller-owned transaction outcome.
func TestScopedKeyedWriteParity(t *testing.T) {
	for _, operation := range []string{"update", "delete"} {
		for _, mode := range []string{"plain", "hooks", "wrong_scope", "cardinality", "unknown"} {
			t.Run(operation+"/"+mode, func(t *testing.T) {
				type transcript struct {
					SQL                        []string
					Args                       [][]any
					Hooks                      []string
					Result                     keyedRow
					NotFound, TooMany, Unknown bool
				}
				{
					var got transcript
					hooks := mode == "hooks"
					c := testConnector{statements: &got.SQL, bindings: &got.Args, columns: []string{"id", "scope", "name"}, respondBound: func(sql string, bound []driver.NamedValue) ([]string, [][]driver.Value, error) {
						if !strings.Contains(sql, `"scope" =`) || !strings.Contains(sql, `"id" =`) {
							t.Errorf("lost scoped identity: %s", sql)
						}
						if mode == "wrong_scope" {
							return []string{"id", "scope", "name"}, nil, nil
						}
						name := "returned"
						if strings.HasPrefix(sql, "SELECT") {
							name = "before"
						}
						rows := [][]driver.Value{{int64(7), "tenant", name}}
						if mode == "cardinality" {
							rows = append(rows, rows[0])
						}
						return []string{"id", "scope", "name"}, rows, nil
					}}
					if mode == "unknown" {
						c.commitErr = errors.New("commit response lost")
					}
					db := hookDB(t, c)
					var row keyedRow
					var err error
					// A caller-owned transaction makes the write's savepoint visible.
					err = db.Transaction(t.Context(), func(tx *database.Tx) error {
						var opts []Option
						if hooks {
							opts = append(opts, WithHooks(func() Hooks[keyedRow] {
								return Hooks[keyedRow]{Updating: func(_ context.Context, _ *database.Tx, b keyedRow, _ *Write[keyedRow]) error {
									got.Hooks = append(got.Hooks, "before:"+b.Name)
									return nil
								}, Updated: func(_ context.Context, _ *database.Tx, c Changes[keyedRow]) error {
									r, _ := c.After().Get()
									got.Hooks = append(got.Hooks, "after:"+r.Name)
									return nil
								}, Deleting: func(_ context.Context, _ *database.Tx, b keyedRow) error {
									got.Hooks = append(got.Hooks, "before:"+b.Name)
									return nil
								}, Deleted: func(_ context.Context, _ *database.Tx, c Changes[keyedRow]) error {
									r, _ := c.Before().Get()
									got.Hooks = append(got.Hooks, "after:"+r.Name)
									return nil
								}}
							}))
						}
						fields := keyedFields(opts...)
						q := fields.Query().Where(TextEq(fields.Scope, "tenant"))
						if operation == "update" {
							row, err = Update(t.Context(), tx, q, fields.Key(), 7, TextSet(fields.Name, "assigned"))
						} else {
							row, err = Delete(t.Context(), tx, q, fields.Key(), 7)
						}
						return err
					})
					got.Result = row
					got.NotFound = errors.Is(err, database.NotFound)
					got.TooMany = errors.Is(err, database.TooManyRows)
					var outcome *database.Error
					got.Unknown = errors.As(err, &outcome) && outcome.Outcome() == database.Unknown
					switch mode {
					case "wrong_scope":
						if !got.NotFound {
							t.Fatalf("wrong scope: %v", err)
						}
					case "cardinality":
						if err == nil {
							t.Fatal("multiple rows accepted")
						}
					case "unknown":
						if !got.Unknown {
							t.Fatalf("unknown commit: %v", err)
						}
					default:
						if err != nil {
							t.Fatal(err)
						}
						if row.Name != "returned" {
							t.Fatalf("lost returning: %+v", row)
						}
					}
					statements := 0
					for _, sql := range got.SQL {
						if strings.HasPrefix(sql, "SELECT") || strings.HasPrefix(sql, "UPDATE") || strings.HasPrefix(sql, "DELETE") {
							statements++
						}
					}
					want := 1
					if hooks {
						want = 2
					}
					if statements != want {
						t.Fatalf("statements %d want %d", statements, want)
					}
					frozenMatch(t, "keyed", got)
				}
			})
		}
	}
}
func TestScopedKeyedWriteRejectsForeignKeyBeforeTransaction(t *testing.T) {
	fields, foreign := keyedFields(), keyedFields()
	begins := 0
	db := hookDB(t, testConnector{onBegin: func() { begins++ }})
	for _, key := range []Key[keyedRow, int64]{foreign.Key(), {}} {
		if _, err := Update(t.Context(), db, fields.Query(), key, 7, TextSet(fields.Name, "x")); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
		if _, err := Delete(t.Context(), db, fields.Query(), key, 7); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
	if begins != 0 {
		t.Fatal("invalid key opened a transaction")
	}
}

func TestScopedKeyedUnknownCandidate(t *testing.T) {
	fields := keyedFields()
	for _, remove := range []bool{false, true} {
		begins := 0
		db := hookDB(t, testConnector{columns: []string{"id", "scope", "name"}, values: [][]driver.Value{{int64(7), "tenant", "returned"}}, commitErr: errors.New("commit response lost"), onBegin: func() { begins++ }})
		var row keyedRow
		var err error
		if remove {
			row, err = Delete(t.Context(), db, fields.Query().Where(TextEq(fields.Scope, "tenant")), fields.Key(), 7)
		} else {
			row, err = Update(t.Context(), db, fields.Query().Where(TextEq(fields.Scope, "tenant")), fields.Key(), 7, TextSet(fields.Name, "assigned"))
		}
		var failure *WriteError[keyedRow]
		var outcome *database.Error
		if row != (keyedRow{}) || !errors.As(err, &failure) || !errors.As(err, &outcome) || outcome.Outcome() != database.Unknown || failure.Candidate().Name != "returned" || begins != 1 {
			t.Fatalf("lost reconciliation or retried write: %v", err)
		}
	}
}
