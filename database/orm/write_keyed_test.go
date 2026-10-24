package orm

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
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

// The old side is the generated Update/Delete expansion from
// emit_model_mutation.go: append primary equality, then Patch/Remove. Its
// execution goes through execute_mutation.go, independently of ORM writeOne.
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
				var results []transcript
				for _, old := range []bool{true, false} {
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
						if old {
							id, text := codec.Signed[int64](), codec.String[string]()
							def := query.Define("keyed", "id", []query.Column{{Name: "id"}, {Name: "scope"}, {Name: "name"}}, func(r database.Row) (keyedRow, error) {
								var m keyedRow
								e := r.Scan(id.Scan(&m.ID), text.Scan(&m.Scope), text.Scan(&m.Name))
								return m, e
							}, query.NewModelField("id", id, func(m keyedRow) int64 { return m.ID }), query.NewModelField("scope", text, func(m keyedRow) string { return m.Scope }), query.NewModelField("name", text, func(m keyedRow) string { return m.Name }))
							if hooks {
								def = def.WithWriteHooks(func() query.WriteHooks[keyedRow] {
									return query.WriteHooks[keyedRow]{Before: func(_ context.Context, _ *database.Tx, _ lifecycle.Operation, before value.Optional[keyedRow], m query.Mutation[keyedRow]) (query.Mutation[keyedRow], error) {
										b, _ := before.Get()
										got.Hooks = append(got.Hooks, "before:"+b.Name)
										return m, nil
									}, After: func(_ context.Context, _ *database.Tx, _ lifecycle.Operation, before, after value.Optional[keyedRow], _ query.Mutation[keyedRow]) error {
										r, _ := after.Get()
										if operation == "delete" {
											r, _ = before.Get()
										}
										got.Hooks = append(got.Hooks, "after:"+r.Name)
										return nil
									}}
								})
							}
							q := query.ForModel(def).Where(query.NewTextField[keyedRow]("keyed", "scope", text).Eq("tenant"), query.NewExactField[keyedRow]("keyed", "id", id).Eq(7))
							if operation == "update" {
								row, err = q.Patch(t.Context(), tx, query.Change(query.Assign[keyedRow]("keyed", "name", text, "assigned")))
							} else {
								row, err = q.Remove(t.Context(), tx)
							}
						} else {
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
							q := fields.Query().Where(fields.Scope.Eq("tenant"))
							if operation == "update" {
								row, err = Update(t.Context(), tx, q, fields.Key(), 7, fields.Name.Set("assigned"))
							} else {
								row, err = Delete(t.Context(), tx, q, fields.Key(), 7)
							}
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
					results = append(results, got)
				}
				if !reflect.DeepEqual(results[0], results[1]) {
					t.Fatalf("old/new mismatch:\n%+v\n%+v", results[0], results[1])
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
		if _, err := Update(t.Context(), db, fields.Query(), key, 7, fields.Name.Set("x")); !errors.Is(err, fault.Invalid) {
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
			row, err = Delete(t.Context(), db, fields.Query().Where(fields.Scope.Eq("tenant")), fields.Key(), 7)
		} else {
			row, err = Update(t.Context(), db, fields.Query().Where(fields.Scope.Eq("tenant")), fields.Key(), 7, fields.Name.Set("assigned"))
		}
		var failure *WriteError[keyedRow]
		var outcome *database.Error
		if row != (keyedRow{}) || !errors.As(err, &failure) || !errors.As(err, &outcome) || outcome.Outcome() != database.Unknown || failure.Candidate().Name != "returned" || begins != 1 {
			t.Fatalf("lost reconciliation or retried write: %v", err)
		}
	}
}
