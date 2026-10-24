package orm

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type pivotKeyInput string
type storedPivotKey int64
type inputKeyParent struct {
	ID    int64
	Code  value.Nullable[storedPivotKey]
	Links relation.Through[inputKeyTarget, inputKeyPivot]
}
type inputKeyTarget struct {
	ID   int64
	Code value.Nullable[storedPivotKey]
}
type inputKeyPivot struct {
	ID                 int64
	ParentID, TargetID value.Nullable[storedPivotKey]
}
type inputKeyParentTable struct {
	Table[inputKeyParent, int64]
	Code  NullableInputNumber[inputKeyParent, pivotKeyInput, storedPivotKey]
	Links Through[inputKeyParent, inputKeyTarget, inputKeyPivot]
}
type inputKeyTargetTable struct {
	Table[inputKeyTarget, int64]
	Code NullableInputNumber[inputKeyTarget, pivotKeyInput, storedPivotKey]
}
type inputKeyPivotTable struct {
	Table[inputKeyPivot, int64]
	ParentID, TargetID NullableInputNumber[inputKeyPivot, pivotKeyInput, storedPivotKey]
}

func TestRelationWriteNullableStoredKeysWithDistinctInputs(t *testing.T) {
	for _, operation := range []string{"attach", "attach_many", "sync", "keep", "toggle", "detach", "sync_existing"} {
		for _, mode := range []string{"omitted", "missing_parent", "missing_target", "explicit", "hook_override", "hook_cannot_fill", "invalid_input"} {
			if (operation == "detach" || operation == "sync_existing") && mode != "omitted" {
				continue
			}
			t.Run(operation+"/"+mode, func(t *testing.T) {
				calls, hooks := 0, 0
				transform := func(in pivotKeyInput) (storedPivotKey, error) {
					calls++
					n, e := strconv.ParseInt(string(in), 10, 64)
					return storedPivotKey(n), e
				}
				a := Define("input_key_parents", func(b Builder[inputKeyParent], m *inputKeyParent) inputKeyParentTable {
					return inputKeyParentTable{TableOf(b, &m.ID), NullableInputNumberOf(b, &m.Code, transform), ThroughOf(b, &m.Links)}
				})
				z := Define("input_key_targets", func(b Builder[inputKeyTarget], m *inputKeyTarget) inputKeyTargetTable {
					return inputKeyTargetTable{TableOf(b, &m.ID), NullableInputNumberOf(b, &m.Code, transform)}
				})
				var p inputKeyPivotTable
				var opts []Option
				if mode == "hook_override" || mode == "hook_cannot_fill" {
					opts = append(opts, WithHooks(func() Hooks[inputKeyPivot] {
						return Hooks[inputKeyPivot]{Creating: func(_ context.Context, _ *database.Tx, w *Write[inputKeyPivot]) error {
							hooks++
							if calls != 0 {
								t.Fatal("mutator ran before hook")
							}
							for _, field := range []struct {
								setter func(pivotKeyInput) Assignment[inputKeyPivot]
								want   pivotKeyInput
							}{{p.ParentID.Set, "91"}, {p.TargetID.Set, "81"}} {
								input, set := WriteNullableGet(w, field.setter).Get()
								v, nonNull := input.Get()
								if !WriteIsSet(w, field.setter) || !set || !nonNull || v != field.want {
									t.Fatal("hook lost typed input", v, set, nonNull)
								}
							}
							return WriteSet(w, p.TargetID.Set("81"))
						}}
					}))
				}
				p = Define("input_key_pivots", func(b Builder[inputKeyPivot], m *inputKeyPivot) inputKeyPivotTable {
					return inputKeyPivotTable{TableOf(b, &m.ID), NullableInputNumberOf(b, &m.ParentID, transform), NullableInputNumberOf(b, &m.TargetID, transform)}
				}, opts...)
				r := ManyToManyOf(a.Links, a.Code, p.ParentID, p.TargetID, z.Code)
				var statements []string
				var binds [][]any
				db := hookDB(t, testConnector{statements: &statements, bindings: &binds, respond: func(s string) ([]string, [][]driver.Value) {
					switch {
					case strings.HasPrefix(s, "SELECT EXISTS("):
						return []string{"exists"}, [][]driver.Value{{true}}
					case strings.HasPrefix(s, "SELECT COUNT(*) FROM ("):
						return []string{"count"}, [][]driver.Value{{int64(1)}}
					case strings.Contains(s, `FROM "input_key_parents"`):
						return make([]string, 2), [][]driver.Value{{int64(1), int64(91)}}
					case strings.Contains(s, `FROM "input_key_targets"`):
						return make([]string, 2), [][]driver.Value{{int64(10), int64(81)}}
					case strings.HasPrefix(s, "SELECT") && strings.Contains(s, `FROM "input_key_pivots"`) && operation != "sync_existing":
						return make([]string, 3), nil
					default:
						return make([]string, 3), [][]driver.Value{{int64(100), int64(91), int64(81)}}
					}
				}})
				source, target := inputKeyParent{ID: 1, Code: value.Of(storedPivotKey(999))}, inputKeyTarget{ID: 10, Code: value.Of(storedPivotKey(888))}
				create := []Assignment[inputKeyPivot]{p.Key().Set(100)}
				switch mode {
				case "missing_parent":
					create = append(create, p.TargetID.Set("81"))
				case "missing_target":
					create = append(create, p.ParentID.Set("91"))
				case "explicit", "hook_override":
					create = append(create, p.ParentID.Set("91"), p.TargetID.Set("81"))
				case "invalid_input":
					create = append(create, p.ParentID.Set("invalid"), p.TargetID.Set("81"))
				}
				var err error
				var attached []inputKeyPivot
				switch operation {
				case "attach":
					var out inputKeyPivot
					out, err = Attach(t.Context(), db, r, source, target, create...)
					if err == nil {
						attached = []inputKeyPivot{out}
					} else if out.ID != 0 {
						t.Fatal("failed attach returned a model", out)
					}
				case "attach_many":
					attached, err = AttachMany(t.Context(), db, r, source, []inputKeyTarget{target}, create...)
				case "sync", "sync_existing", "keep", "toggle":
					var out PivotChanges[inputKeyPivot]
					switch operation {
					case "sync", "sync_existing":
						out, err = Sync(t.Context(), db, r, source, []inputKeyTarget{target}, create, nil)
					case "keep":
						out, err = SyncWithoutDetaching(t.Context(), db, r, source, []inputKeyTarget{target}, create, nil)
					case "toggle":
						out, err = Toggle(t.Context(), db, r, source, []inputKeyTarget{target}, create...)
					}
					attached = out.Attached
				case "detach":
					var out []inputKeyPivot
					out, err = Detach(t.Context(), db, r, source, target)
					if err == nil && len(out) != 1 {
						t.Fatal(out)
					}
				}
				readsOnly := operation == "detach" || operation == "sync_existing"
				omitted := mode == "omitted" || mode == "missing_parent" || mode == "missing_target" || mode == "hook_cannot_fill"
				wantStatements, wantCalls, wantHooks := 5, 2, 0
				if operation == "sync" || operation == "keep" || operation == "toggle" {
					wantStatements++
				}
				if mode == "hook_override" {
					wantHooks = 1
				}
				switch {
				case readsOnly:
					if err != nil || len(attached) != 0 {
						t.Fatal(attached, err)
					}
					wantStatements, wantCalls, wantHooks = 4, 0, 0
				case omitted || mode == "invalid_input":
					if err == nil || len(attached) != 0 {
						t.Fatal("accepted missing/invalid input", attached, err)
					}
					if omitted && !errors.Is(err, fault.Invalid) {
						t.Fatal(err)
					}
					var candidate *WriteError[inputKeyPivot]
					if errors.As(err, &candidate) {
						t.Fatal("pre-write failure exposed candidate")
					}
					wantStatements -= 2
					wantCalls, wantHooks = 0, 0
					if mode == "invalid_input" {
						wantCalls = 1
					}
				default:
					if err != nil || len(attached) != 1 {
						t.Fatal(attached, err)
					}
					if attached[0].ParentID != value.Of(storedPivotKey(91)) || attached[0].TargetID != value.Of(storedPivotKey(81)) {
						t.Fatal(attached)
					}
				}
				if calls != wantCalls || hooks != wantHooks || len(statements) != wantStatements {
					t.Fatal("calls, hooks, statements", calls, hooks, statements)
				}
				for i, s := range statements {
					if strings.HasPrefix(s, "INSERT") {
						if omitted || mode == "invalid_input" {
							t.Fatal("invalid input executed INSERT", statements)
						}
						if fmt.Sprint(binds[i]) != "[100 91 81]" {
							t.Fatal("stored keys changed", binds[i])
						}
					}
				}
			})
		}
	}
}

type softWriteParent struct {
	ID        int64
	DeletedAt value.Nullable[temporal.DateTime]
	Links     relation.Through[writeTarget, writePivot]
}
type softWriteParentTable struct {
	Table[softWriteParent, int64]
	DeletedAt SoftDelete[softWriteParent, temporal.DateTime]
	Links     Through[softWriteParent, writeTarget, writePivot]
}

func TestAttachRechecksBothEndpointIdentitiesAfterHooks(t *testing.T) {
	for _, endpoint := range []string{"source", "target"} {
		t.Run(endpoint, func(t *testing.T) {
			a := Define("soft_write_parents", func(b Builder[softWriteParent], m *softWriteParent) softWriteParentTable {
				return softWriteParentTable{TableOf(b, &m.ID), SoftDeleteOf(b, &m.DeletedAt), ThroughOf(b, &m.Links)}
			})
			hookRan := false
			_, z, p, _ := writeRelationTables(WithHooks(func() Hooks[writePivot] {
				return Hooks[writePivot]{Created: func(ctx context.Context, tx *database.Tx, _ Changes[writePivot]) error {
					// A mutation on this same transaction must be visible to the postcondition.
					sql, key := `UPDATE "soft_write_parents" SET "deleted_at" = CURRENT_TIMESTAMP WHERE "id" = $1`, int64(1)
					if endpoint == "target" {
						sql, key = `DELETE FROM "write_targets" WHERE "id" = $1`, int64(10)
					}
					_, err := tx.Exec(ctx, sql, key)
					hookRan = err == nil
					return err
				}}
			}))
			var statements []string
			var binds [][]any
			db := hookDB(t, testConnector{statements: &statements, bindings: &binds, respond: func(s string) ([]string, [][]driver.Value) {
				if strings.HasPrefix(s, "SELECT EXISTS(") {
					if !hookRan {
						t.Fatal("postcondition ran before lifecycle")
					}
					if !strings.Contains(s, `"soft_write_parents"."deleted_at" IS NULL`) || !strings.Contains(s, `"soft_write_parents"."id" =`) || !strings.Contains(s, `"foundry_related"."id" =`) {
						t.Fatal("postcondition lost endpoint identity or visibility", s)
					}
					return []string{"exists"}, [][]driver.Value{{false}}
				}
				if strings.Contains(s, `FROM "soft_write_parents"`) {
					return make([]string, 2), [][]driver.Value{{int64(1), nil}}
				}
				return pivotTestReply(1)(s)
			}})
			// A non-primary relation key ensures the target check uses its original ID,
			// rather than allowing a replacement target with the same relation key.
			r := ManyToManyOf(a.Links, a.Key(), p.ParentID, p.TargetID, z.Code)
			out, err := Attach(t.Context(), db, r, softWriteParent{ID: 1}, writeTarget{ID: 10}, p.ID.Set(100), p.Role.Set("member"))
			if !errors.Is(err, fault.Invalid) || out.ID != 0 || !hookRan {
				t.Fatal(out, err, hookRan)
			}
			if len(statements) != 6 {
				t.Fatal(statements)
			}
			if got := fmt.Sprint(binds[len(binds)-1]); !strings.Contains(got, "10") {
				t.Fatal("missing original target identity", got)
			}
		})
	}
}

func TestRelationWriteLimitClassification(t *testing.T) {
	for _, mode := range []string{"current_links", "hook_candidates", "returning", "endpoint_cardinality"} {
		t.Run(mode, func(t *testing.T) {
			var opts []Option
			if mode == "hook_candidates" {
				opts = append(opts, WithHooks(func() Hooks[writePivot] { return Hooks[writePivot]{} }))
			}
			_, _, _, r := writeRelationTables(opts...)
			var statements []string
			db := hookDB(t, testConnector{statements: &statements, respond: func(s string) ([]string, [][]driver.Value) {
				if mode == "endpoint_cardinality" && strings.Contains(s, `FROM "write_targets"`) {
					return make([]string, 2), [][]driver.Value{{int64(10), int64(81)}, {int64(10), int64(81)}}
				}
				if strings.Contains(s, `FROM "write_pivots"`) || strings.HasPrefix(s, "UPDATE") {
					return make([]string, 5), pivotTestRows(2)
				}
				return pivotTestReply(1)(s)
			}})
			r = WithRelationWriteLimit(r, 1)
			var err error
			if mode == "current_links" {
				_, err = DetachAll(t.Context(), db, r, writeParent{ID: 1})
			} else {
				_, err = Detach(t.Context(), db, r, writeParent{ID: 1}, writeTarget{ID: 10})
			}
			if mode == "endpoint_cardinality" {
				if !errors.Is(err, database.TooManyRows) {
					t.Fatal(err)
				}
			} else if !errors.Is(err, fault.Invalid) || errors.Is(err, database.TooManyRows) {
				t.Fatal(err)
			}
			if mode == "current_links" || mode == "hook_candidates" {
				for _, s := range statements {
					if strings.HasPrefix(s, "UPDATE") || strings.HasPrefix(s, "DELETE") {
						t.Fatal("limit failure mutated pivots", statements)
					}
				}
			}
		})
	}
}

func TestRelationWriteSameTypeDefaultsStillRunMutators(t *testing.T) {
	for _, bulk := range []bool{false, true} {
		t.Run(fmt.Sprint(bulk), func(t *testing.T) {
			calls := 0
			mutate := func(v int64) (int64, error) { calls++; return v, nil }
			a, z, _, _ := writeRelationTables()
			type inputPivotTable struct {
				Table[writePivot, int64]
				ID        Key[writePivot, int64]
				ParentID  InputNumber[writePivot, int64, int64]
				TargetID  InputNumber[writePivot, int64, int64]
				Role      Text[writePivot, string]
				DeletedAt SoftDelete[writePivot, temporal.DateTime]
			}
			p := Define("write_pivots", func(b Builder[writePivot], m *writePivot) inputPivotTable {
				table := TableOf(b, &m.ID)
				return inputPivotTable{table, table.Key(), InputNumberOf(b, &m.ParentID, mutate), InputNumberOf(b, &m.TargetID, mutate), TextOf(b, &m.Role), SoftDeleteOf(b, &m.DeletedAt)}
			})
			r := ManyToManyOf(a.Links, a.ID, p.ParentID, p.TargetID, z.ID)
			var statements []string
			db := hookDB(t, testConnector{statements: &statements, respond: pivotTestReply(1)})
			var err error
			if bulk {
				_, err = AttachMany(t.Context(), db, r, writeParent{ID: 1}, []writeTarget{{ID: 10}}, p.ID.Set(100), p.Role.Set("member"))
			} else {
				_, err = Attach(t.Context(), db, r, writeParent{ID: 1}, writeTarget{ID: 10}, p.ID.Set(100), p.Role.Set("member"))
			}
			if err != nil || calls != 2 || len(statements) != 4 {
				t.Fatal(err, calls, statements)
			}
		})
	}
}
