package orm_test

import "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

type unsetRow struct {
	ID       int64
	Revision int64 `foundry:"default=database"`
	Note     value.Nullable[string]
	Name     string
}
type unsetTable struct {
	orm.Table[unsetRow, int64]
	Revision orm.InputNumber[unsetRow, int64, int64]
	Note     orm.NullableInputText[unsetRow, string, string]
	Name     orm.Text[unsetRow, string]
}

func unsetFields(name string, mutations *int, factory func() orm.Hooks[unsetRow]) unsetTable {
	return orm.Define(name, func(b orm.Builder[unsetRow], m *unsetRow) unsetTable {
		return unsetTable{orm.TableOf(b, &m.ID), orm.InputNumberOf(b, &m.Revision, func(v int64) (int64, error) { *mutations++; return v, nil }), orm.NullableInputTextOf(b, &m.Note, func(v string) (string, error) { *mutations++; return v, nil }), orm.TextOf(b, &m.Name)}
	}, orm.WithHooks(factory))
}

func TestWriteUnsetPreservesOmissionThroughSQLAndChanges(t *testing.T) {
	for _, create := range []bool{false, true} {
		for _, null := range []bool{false, true} {
			t.Run(map[bool]string{false: "update", true: "create"}[create]+map[bool]string{false: "/value", true: "/null"}[null], func(t *testing.T) {
				mutations := 0
				var fields unsetTable
				var held *orm.Write[unsetRow]
				var captured orm.Changes[unsetRow]
				unset := true
				fields = unsetFields("unset_rows", &mutations, func() orm.Hooks[unsetRow] {
					return orm.Hooks[unsetRow]{
						Saving: func(_ context.Context, _ *database.Tx, _ value.Optional[unsetRow], w *orm.Write[unsetRow]) error {
							held = w
							if !unset {
								return nil
							}
							if !orm.WriteIsSet(w, func() func(arg0 int64) orm.Assignment[unsetRow] {
								receiver := fields.Revision
								return func(arg0 int64) orm.Assignment[unsetRow] { return orm.InputNumberSet(receiver, arg0) }
							}()) || !orm.WriteIsSet(w, func() func(arg0 string) orm.Assignment[unsetRow] {
								receiver := fields.Note
								return func(arg0 string) orm.Assignment[unsetRow] { return orm.NullableInputTextSet(receiver, arg0) }
							}()) {
								return errors.New("supplied zero or nullable input was omitted")
							}
							if err := orm.WriteUnset(w, func() func(arg0 int64) orm.Assignment[unsetRow] {
								receiver := fields.Revision
								return func(arg0 int64) orm.Assignment[unsetRow] { return orm.InputNumberSet(receiver, arg0) }
							}()); err != nil {
								return err
							}
							if err := orm.WriteUnset(w, func() func(arg0 string) orm.Assignment[unsetRow] {
								receiver := fields.Note
								return func(arg0 string) orm.Assignment[unsetRow] { return orm.NullableInputTextSet(receiver, arg0) }
							}()); err != nil {
								return err
							}
							if err := orm.WriteUnset(w, func() func(arg0 string) orm.Assignment[unsetRow] {
								receiver := fields.Note
								return func(arg0 string) orm.Assignment[unsetRow] { return orm.NullableInputTextSet(receiver, arg0) }
							}()); err != nil {
								return err
							}
							if orm.WriteGet(w, func() func(arg0 int64) orm.Assignment[unsetRow] {
								receiver := fields.Revision
								return func(arg0 int64) orm.Assignment[unsetRow] { return orm.InputNumberSet(receiver, arg0) }
							}()).IsSet() || orm.WriteNullableGet(w, func() func(arg0 string) orm.Assignment[unsetRow] {
								receiver := fields.Note
								return func(arg0 string) orm.Assignment[unsetRow] { return orm.NullableInputTextSet(receiver, arg0) }
							}()).IsSet() || orm.WriteIsSet(w, func() func(arg0 string) orm.Assignment[unsetRow] {
								receiver := fields.Note
								return func(arg0 string) orm.Assignment[unsetRow] { return orm.NullableInputTextSet(receiver, arg0) }
							}()) {
								return errors.New("unset retained an assignment")
							}
							return nil
						}, Saved: func(_ context.Context, _ *database.Tx, c orm.Changes[unsetRow]) error { captured = c; return nil },
					}
				})
				note := orm.NullableInputTextSet(fields.Note, "submitted")
				if null {
					note = orm.NullableInputTextSetNull(fields.Note)
				}
				input := []orm.Assignment[unsetRow]{orm.InputNumberSet(fields.Revision, 0), note, orm.TextSet(fields.Name, "changed")}
				var statements []string
				db := hookDB(t, testConnector{statements: &statements, columns: []string{"id", "revision", "note", "name"}, values: [][]driver.Value{{int64(1), int64(42), "stored", "changed"}}})
				var err error
				if create {
					_, err = orm.Insert(t.Context(), db, fields.Table, append([]orm.Assignment[unsetRow]{orm.KeySet(fields.Key(), 1)}, input...)...)
				} else {
					_, err = orm.Update(t.Context(), db, fields.Table.Query(), fields.Table.Key(), 1, input...)
				}
				if err != nil {
					t.Fatal(err)
				}
				if mutations != 0 {
					t.Fatal("omitted values reached mutators", mutations)
				}
				for _, sql := range statements {
					if strings.HasPrefix(sql, "INSERT") || strings.HasPrefix(sql, "UPDATE") {
						assignmentSQL := strings.Split(sql, " RETURNING ")[0]
						if strings.Contains(assignmentSQL, `"revision"`) || strings.Contains(assignmentSQL, `"note"`) {
							t.Fatal("unset field was sent to SQL", assignmentSQL)
						}
					}
				}
				revision, err := orm.ChangeOf(captured, orm.InputNumberExpr(fields.Revision))
				if err != nil {
					t.Fatal(err)
				}
				nullable, err := orm.NullableChangeOf(captured, orm.NullableInputTextExpr(fields.Note))
				if err != nil {
					t.Fatal(err)
				}
				if revision.Assigned() || nullable.Assigned() {
					t.Fatal("unset field remained assigned in stored changes")
				}
				after, _ := captured.After().Get()
				if after.Revision != 42 || after.Note != value.Of("stored") {
					t.Fatal("unset fabricated zero or NULL")
				}
				if err := orm.WriteUnset(held, func() func(arg0 int64) orm.Assignment[unsetRow] {
					receiver := fields.Revision
					return func(arg0 int64) orm.Assignment[unsetRow] { return orm.InputNumberSet(receiver, arg0) }
				}()); !errors.Is(err, fault.Invalid) {
					t.Fatal("expired draft accepted unset", err)
				}
				// Reusing caller input proves removal did not alter the caller's assignments.
				unset = false
				if _, err := orm.Update(t.Context(), db, fields.Table.Query(), fields.Table.Key(), 1, input...); err != nil {
					t.Fatal(err)
				}
				want := 2
				if null {
					want = 1
				}
				if mutations != want {
					t.Fatal("caller input changed or explicit NULL ran its mutator", mutations)
				}
			})
		}
	}
}

func TestWriteUnsetCanBeReassignedAndRejectsForeignAccess(t *testing.T) {
	for _, mode := range []string{"reassign", "foreign", "nil setter"} {
		t.Run(mode, func(t *testing.T) {
			mutations := 0
			var fields unsetTable
			foreign := unsetFields("another_unset_rows", &mutations, nil)
			fields = unsetFields("unset_rows", &mutations, func() orm.Hooks[unsetRow] {
				return orm.Hooks[unsetRow]{Creating: func(_ context.Context, _ *database.Tx, w *orm.Write[unsetRow]) error {
					if mode == "foreign" {
						_ = orm.WriteUnset(w, func() func(arg0 int64) orm.Assignment[unsetRow] {
							receiver := foreign.Revision
							return func(arg0 int64) orm.Assignment[unsetRow] { return orm.InputNumberSet(receiver, arg0) }
						}())
						return nil
					}
					if mode == "nil setter" {
						_ = orm.WriteUnset[unsetRow, int64](w, nil)
						return nil
					}
					if err := orm.WriteUnset(w, func() func(arg0 int64) orm.Assignment[unsetRow] {
						receiver := fields.Revision
						return func(arg0 int64) orm.Assignment[unsetRow] { return orm.InputNumberSet(receiver, arg0) }
					}()); err != nil {
						return err
					}
					if err := orm.WriteSet(w, orm.InputNumberSet(fields.Revision, 0), orm.NullableInputTextSetNull(fields.Note)); err != nil {
						return err
					}
					revision, ok := orm.WriteGet(w, func() func(arg0 int64) orm.Assignment[unsetRow] {
						receiver := fields.Revision
						return func(arg0 int64) orm.Assignment[unsetRow] { return orm.InputNumberSet(receiver, arg0) }
					}()).Get()
					note, present := orm.WriteNullableGet(w, func() func(arg0 string) orm.Assignment[unsetRow] {
						receiver := fields.Note
						return func(arg0 string) orm.Assignment[unsetRow] { return orm.NullableInputTextSet(receiver, arg0) }
					}()).Get()
					if !ok || revision != 0 || !present || !note.IsNull() {
						return errors.New("reassignment lost explicit zero/NULL")
					}
					return nil
				}}
			})
			writes := 0
			db := hookDB(t, testConnector{respond: func(sql string) ([]string, [][]driver.Value) {
				if strings.HasPrefix(sql, "INSERT") {
					writes++
				}
				return []string{"id", "revision", "note", "name"}, [][]driver.Value{{int64(1), int64(0), nil, "name"}}
			}})
			_, err := orm.Insert(t.Context(), db, fields.Table, orm.KeySet(fields.Key(), 1), orm.TextSet(fields.Name, "name"), orm.InputNumberSet(fields.Revision, 2))
			if mode == "reassign" {
				if err != nil || writes != 1 || mutations != 1 {
					t.Fatal("reassignment failed", err, writes, mutations)
				}
			} else if !errors.Is(err, fault.Invalid) || writes != 0 || mutations != 0 {
				t.Fatal("invalid access reached SQL or mutators", err, writes, mutations)
			}
		})
	}
	if err := orm.WriteUnset[unsetRow, int64](nil, nil); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil draft accepted", err)
	}
}
