package orm

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/encrypted"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/lifecore"
	"github.com/weiloon1234/Foundry-Go/value"
)

type hookedInput struct{ Text string }
type hookedStored string
type hookedRow struct {
	ID        int64
	Name      string `foundry:"default=database"`
	Note      value.Nullable[hookedStored]
	Body      []byte
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt value.Nullable[time.Time]
	Scratch   string `foundry:"-"`
}
type hookedTable struct {
	Table[hookedRow, int64]
	Name    InputText[hookedRow, string, string]
	Note    NullableInputText[hookedRow, hookedInput, hookedStored]
	Body    Column[hookedRow, []byte]
	Created CreatedAt[hookedRow, time.Time]
	Updated UpdatedAt[hookedRow, time.Time]
	Deleted SoftDelete[hookedRow, time.Time]
}

func hookedFields(factory func() Hooks[hookedRow], extra ...Option) hookedTable {
	opts := append([]Option{}, extra...)
	if factory != nil {
		opts = append(opts, WithHooks(factory))
	}
	return Define("hooked", func(b Builder[hookedRow], m *hookedRow) hookedTable {
		deleted := SoftDelete[hookedRow, time.Time]{}
		disabled := false
		for _, opt := range extra {
			disabled = disabled || opt.noSoftDeletes
		}
		if disabled {
			deleted.nullableTime = NullableTimeOf(b, &m.DeletedAt).nullableTime
		} else {
			deleted = SoftDeleteOf(b, &m.DeletedAt)
		}
		return hookedTable{TableOf(b, &m.ID), InputTextOf(b, &m.Name, func(s string) (string, error) { return strings.TrimSpace(s), nil }), NullableInputTextOf(b, &m.Note, func(v hookedInput) (hookedStored, error) { return hookedStored(strings.TrimSpace(v.Text)), nil }), BytesOf(b, &m.Body), CreatedAtOf(b, &m.CreatedAt), UpdatedAtOf(b, &m.UpdatedAt), deleted}
	}, opts...)
}
func hookDB(t *testing.T, c testConnector, declarations ...lifecycle.Declaration) *database.DB {
	t.Helper()
	db, err := database.Prepare(database.Adapter{Connector: c}, database.DefaultPoolConfig(), database.WithClock(fixedNow{time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	set, err := lifecycle.NewObservers(declarations...)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.BindObservers(set); err != nil {
		t.Fatal(err)
	}
	if err = db.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	return db
}

var hookColumns = []string{"id", "name", "note", "body", "created_at", "updated_at", "deleted_at"}

func hookValues(name string, note driver.Value, deleted driver.Value) [][]driver.Value {
	now := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	return [][]driver.Value{{int64(1), name, note, []byte{1, 2}, now, now, deleted}}
}

func TestHooksStageOrderLockedSnapshotsAndReturning(t *testing.T) {
	var trace []string
	var q hookedTable
	closed := 0
	var saved Changes[hookedRow]
	factories := 0
	factory := func(name string) func() Hooks[hookedRow] {
		return func() Hooks[hookedRow] {
			factories++
			trace = append(trace, name+".factory")
			if closed == 0 {
				t.Error("factory before locked rows closed")
			}
			return Hooks[hookedRow]{
				Saving: func(ctx context.Context, tx *database.Tx, before value.Optional[hookedRow], w *Write[hookedRow]) error {
					trace = append(trace, name+".saving")
					b, _ := before.Get()
					if b.Name != "old" || b.Scratch != "" {
						t.Error("original")
					}
					input := WriteNullableGet(w, func() func(arg0 hookedInput) Assignment[hookedRow] {
						receiver := q.Note
						return func(arg0 hookedInput) Assignment[hookedRow] { return NullableInputTextSet(receiver, arg0) }
					}())
					v, ok := input.Get()
					if !ok || v.IsNull() {
						t.Error("nullable input lost")
					}
					if !WriteIsSet(w, func() func(arg0 string) Assignment[hookedRow] {
						receiver := q.Name
						return func(arg0 string) Assignment[hookedRow] { return InputTextSet(receiver, arg0) }
					}()) {
						t.Error("assignment lost")
					}
					raw, _ := WriteGet(w, func() func(arg0 string) Assignment[hookedRow] {
						receiver := q.Name
						return func(arg0 string) Assignment[hookedRow] { return InputTextSet(receiver, arg0) }
					}()).Get()
					if raw != " new " {
						t.Error("getter ran mutator")
					}
					return nil
				},
				Updating: func(ctx context.Context, tx *database.Tx, before hookedRow, w *Write[hookedRow]) error {
					trace = append(trace, name+".updating")
					before.Body[0] = 99
					return nil
				},
				Updated: func(ctx context.Context, tx *database.Tx, c Changes[hookedRow]) error {
					trace = append(trace, name+".updated")
					if closed != 2 {
						t.Error("after hook before RETURNING closed")
					}
					nameChange, err := ChangeOf(c, InputTextExpr(q.Name))
					if err != nil || !nameChange.Assigned() || !nameChange.Changed() {
						t.Error("change", err)
					}
					after, _ := c.After().Get()
					if after.Name != "trigger" || after.Body[0] != 1 {
						t.Error("RETURNING/snapshot ownership")
					}
					note, err := NullableChangeOf(c, NullableInputTextExpr(q.Note))
					if err != nil || !note.Assigned() {
						t.Error(err)
					}
					saved = c
					after.Body[0] = 9
					return nil
				},
				Saved: func(context.Context, *database.Tx, Changes[hookedRow]) error {
					trace = append(trace, name+".saved")
					return nil
				},
				AfterCommit: func(ctx context.Context, op lifecycle.Operation, c Changes[hookedRow]) error {
					trace = append(trace, name+".commit")
					if op != lifecycle.Update {
						t.Error("operation")
					}
					return nil
				},
			}
		}
	}
	q = hookedFields(factory("local"))
	declaration, err := NewObserver[hookedRow]("audit").Declare(factory("observer"))
	if err != nil {
		t.Fatal(err)
	}
	var statements []string
	db := hookDB(t, testConnector{statements: &statements, onClose: func() { closed++ }, respond: func(sql string) ([]string, [][]driver.Value) {
		if strings.HasPrefix(sql, "SELECT") {
			return hookColumns, hookValues("old", nil, nil)
		}
		return hookColumns, hookValues("trigger", "new note", nil)
	}}, declaration)
	if err = Check(q); err != nil {
		t.Fatal(err)
	}
	if factories != 0 {
		t.Fatal("eager factory")
	}
	_, err = Update(t.Context(), db, q.Table.Query(), q.Table.Key(), 1, InputTextSet(q.Name, " new "), NullableInputTextSet(q.Note, hookedInput{" new note "}))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"local.factory", "observer.factory", "local.saving", "observer.saving", "local.updating", "observer.updating", "local.updated", "observer.updated", "local.saved", "observer.saved", "local.commit", "observer.commit"}
	if !reflect.DeepEqual(trace, want) {
		t.Fatal(trace)
	}
	if len(statements) != 2 || !strings.Contains(statements[0], "LIMIT $2 FOR UPDATE") {
		t.Fatal(statements)
	}
	b, _ := saved.Before().Get()
	a, _ := saved.After().Get()
	if b.Body[0] != 1 || a.Body[0] != 1 {
		t.Fatal("mutable snapshot escaped")
	}
	if strings.Contains(fmt.Sprintf("%+v %#v", saved, saved), "trigger") {
		t.Fatal("snapshot diagnostic leak")
	}
}

func TestObserverOnlyWritesAndSetAcknowledgement(t *testing.T) {
	q := hookedFields(nil)
	calls := 0
	declaration, err := NewObserver[hookedRow]("observer.only").Declare(func() Hooks[hookedRow] {
		calls++
		return Hooks[hookedRow]{Creating: func(ctx context.Context, tx *database.Tx, w *Write[hookedRow]) error {
			return WriteSet(w, InputTextSet(q.Name, " supplied "))
		}}
	})
	if err != nil {
		t.Fatal(err)
	}
	var statements []string
	db := hookDB(t, testConnector{statements: &statements, columns: hookColumns, values: hookValues("supplied", nil, nil)}, declaration)
	_, err = Insert(t.Context(), db, q.Table, KeySet(q.Key(), 1), ColumnSet(q.Body, []byte{1, 2}))
	if err != nil || calls != 1 {
		t.Fatal(err, calls)
	}
	if _, err = UpdateAll(t.Context(), db, q.Query(), InputTextSet(q.Name, "x")); !errors.Is(err, fault.Invalid) {
		t.Fatal("set silently skipped observers", err)
	}
	if _, err = UpdateAll(t.Context(), db, WithoutModelHooks(q.Query()), InputTextSet(q.Name, "x")); err != nil || calls != 1 {
		t.Fatal(err, calls)
	}
	if _, err = writeOne[hookedRow](t.Context(), db, WithoutModelHooks(q.Query()).state, 0, []Assignment[hookedRow]{KeySet(q.Key(), 1)}); !errors.Is(err, fault.Invalid) {
		t.Fatal("ack disabled local hooks", err)
	}
	other := hookDB(t, testConnector{columns: hookColumns, values: hookValues("database", nil, nil)})
	_, err = Insert(t.Context(), other, q.Table, KeySet(q.Key(), 2), ColumnSet(q.Body, []byte{1, 2}))
	if err != nil || calls != 1 {
		t.Fatal("observer registry leaked", err, calls)
	}
}

func TestSpecialHookStagesAndForceDeleteVisibility(t *testing.T) {
	for _, op := range []lifecycle.Operation{lifecycle.Create, lifecycle.Delete, lifecycle.SoftDelete, lifecycle.Restore, lifecycle.ForceDelete} {
		t.Run(op.String(), func(t *testing.T) {
			var trace []string
			stage := func(name string) func(context.Context, *database.Tx, hookedRow) error {
				return func(context.Context, *database.Tx, hookedRow) error { trace = append(trace, name); return nil }
			}
			after := func(name string) func(context.Context, *database.Tx, Changes[hookedRow]) error {
				return func(ctx context.Context, tx *database.Tx, c Changes[hookedRow]) error {
					trace = append(trace, name)
					if c.After().IsSet() == (op == lifecycle.ForceDelete || op == lifecycle.Delete) {
						t.Error("existence")
					}
					return nil
				}
			}
			q := hookedFields(func() Hooks[hookedRow] {
				return Hooks[hookedRow]{Creating: func(context.Context, *database.Tx, *Write[hookedRow]) error {
					trace = append(trace, "creating")
					return nil
				}, Deleting: stage("deleting"), ForceDeleting: stage("force deleting"), Restoring: stage("restoring"), Created: after("created"), Deleted: after("deleted"), ForceDeleted: after("force deleted"), Restored: after("restored"), Saved: after("saved"), AfterCommit: func(context.Context, lifecycle.Operation, Changes[hookedRow]) error {
					trace = append(trace, "commit")
					return nil
				}}
			})
			var statements []string
			db := hookDB(t, testConnector{statements: &statements, columns: hookColumns, values: hookValues("name", nil, nil)})
			var err error
			var want []string
			switch op {
			case lifecycle.Create:
				_, err = Insert(t.Context(), db, q.Table, KeySet(q.Key(), 1), ColumnSet(q.Body, []byte{1, 2}))
				want = []string{"creating", "created", "saved", "commit"}
			case lifecycle.Delete:
				q = hookedFields(func() Hooks[hookedRow] {
					return Hooks[hookedRow]{Deleting: stage("deleting"), Deleted: after("deleted"), AfterCommit: func(context.Context, lifecycle.Operation, Changes[hookedRow]) error {
						trace = append(trace, "commit")
						return nil
					}}
				}, NoSoftDeletes())
				_, err = Delete(t.Context(), db, q.Table.Query(), q.Table.Key(), 1)
				want = []string{"deleting", "deleted", "commit"}
			case lifecycle.SoftDelete:
				_, err = Delete(t.Context(), db, q.Table.Query(), q.Table.Key(), 1)
				want = []string{"deleting", "deleted", "commit"}
			case lifecycle.Restore:
				_, err = Restore(t.Context(), db, q.Table, 1)
				want = []string{"restoring", "restored", "commit"}
			case lifecycle.ForceDelete:
				_, err = ForceDelete(t.Context(), db, q.Query().OnlyTrashed(), SoftDeletes(q.Deleted), q.Key(), int64(1))
				want = []string{"force deleting", "deleting", "deleted", "force deleted", "commit"}
				for _, sql := range statements {
					if !strings.Contains(sql, `"deleted_at" IS NOT NULL`) {
						t.Error("visibility lost", sql)
					}
				}
			}
			if err != nil || !reflect.DeepEqual(trace, want) {
				t.Fatal(err, trace)
			}
		})
	}
}

func TestHookFaultsRollbackAndNoCandidate(t *testing.T) {
	veto := errors.New("veto")
	for _, phase := range []string{"factory", "before", "after", "commit"} {
		for _, mode := range []string{"error", "panic", "goexit"} {
			t.Run(phase+"/"+mode, func(t *testing.T) {
				fail := func() error {
					switch mode {
					case "panic":
						panic("private")
					case "goexit":
						runtime.Goexit()
					}
					return veto
				}
				commits := 0
				q := hookedFields(func() Hooks[hookedRow] {
					if phase == "factory" {
						_ = fail()
					}
					return Hooks[hookedRow]{Creating: func(context.Context, *database.Tx, *Write[hookedRow]) error {
						if phase == "before" {
							return fail()
						}
						return nil
					}, Created: func(context.Context, *database.Tx, Changes[hookedRow]) error {
						if phase == "after" {
							return fail()
						}
						return nil
					}, AfterCommit: func(context.Context, lifecycle.Operation, Changes[hookedRow]) error {
						commits++
						if phase == "commit" {
							return fail()
						}
						return nil
					}}
				})
				if phase == "factory" && mode == "error" {
					return
				} // Factories return hooks, not an error.
				db := hookDB(t, testConnector{columns: hookColumns, values: hookValues("name", nil, nil)})
				got, err := Insert(t.Context(), db, q.Table, KeySet(q.Key(), 1), ColumnSet(q.Body, []byte{1, 2}))
				if err == nil || got.ID != 0 {
					t.Fatal("callback fault published", err)
				}
				var candidate *WriteError[hookedRow]
				if phase == "commit" {
					if !errors.As(err, &candidate) || candidate.Candidate().ID != 1 {
						t.Fatal("committed candidate lost", err)
					}
				} else if errors.As(err, &candidate) || commits != 0 {
					t.Fatal("aborted write has candidate/aftercommit", err)
				}
				if mode == "error" && !errors.Is(err, veto) {
					t.Fatal("error identity lost", err)
				}
			})
		}
	}
}

func TestHookSavepointsDiscardAndMergeAfterCommit(t *testing.T) {
	commits := 0
	q := hookedFields(func() Hooks[hookedRow] {
		return Hooks[hookedRow]{AfterCommit: func(context.Context, lifecycle.Operation, Changes[hookedRow]) error { commits++; return nil }}
	})
	db := hookDB(t, testConnector{columns: hookColumns, values: hookValues("name", nil, nil)})
	veto := errors.New("rollback child")
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		err := tx.Transaction(t.Context(), func(child *database.Tx) error {
			_, e := Insert(t.Context(), child, q.Table, KeySet(q.Key(), 1), ColumnSet(q.Body, []byte{1, 2}))
			if e != nil {
				return e
			}
			return veto
		})
		if !errors.Is(err, veto) {
			return fmt.Errorf("child: %w", err)
		}
		_, err = Insert(t.Context(), tx, q.Table, KeySet(q.Key(), 1), ColumnSet(q.Body, []byte{1, 2}))
		if err != nil {
			return err
		}
		if commits != 0 {
			return errors.New("aftercommit ran before outer commit")
		}
		return nil
	})
	if err != nil || commits != 1 {
		t.Fatal(err, commits)
	}
}

func TestChangesCanonicalAssignedPersistedAndDraftInputs(t *testing.T) {
	q := hookedFields(nil)
	before := hookedRow{ID: 1, Name: "same", Body: []byte{1, 2}, Scratch: "ignored"}
	c, err := Compare(q.Table, value.Set(before), value.Set(before), InputTextSet(q.Name, "same"), NullableInputTextSetNull(q.Note))
	if err != nil {
		t.Fatal(err)
	}
	body, err := ChangeOf(c, ColumnExpr(q.Body))
	if err != nil {
		t.Fatal(err)
	}
	bodyBefore, _ := body.Before().Get()
	bodyBefore[0] = 9
	bodyBefore, _ = body.Before().Get()
	if bodyBefore[0] != 1 {
		t.Fatal("field-change buffer alias")
	}
	name, err := ChangeOf(c, InputTextExpr(q.Name))
	if err != nil || !name.Assigned() || name.Changed() || c.Changed() || !c.Assigned() || c.Operation().IsSet() {
		t.Fatal("assignment/change", err)
	}
	b, _ := c.Before().Get()
	if b.Scratch != "" {
		t.Fatal("ignored field captured")
	}
	b.Body[0] = 9
	b, _ = c.Before().Get()
	if b.Body[0] != 1 {
		t.Fatal("snapshot alias")
	}
	if _, err = Compare(q.Table, value.Set(before), value.Set(before), KeySet(q.Key(), 1)); !errors.Is(err, fault.Invalid) {
		t.Fatal("key assignment", err)
	}
	var captured *Write[hookedRow]
	q = hookedFields(func() Hooks[hookedRow] {
		return Hooks[hookedRow]{Creating: func(ctx context.Context, tx *database.Tx, w *Write[hookedRow]) error {
			captured = w
			if WriteIsSet(w, func() func(arg0 string) Assignment[hookedRow] {
				receiver := q.Name
				return func(arg0 string) Assignment[hookedRow] { return InputTextSet(receiver, arg0) }
			}()) || WriteGet(w, func() func(arg0 string) Assignment[hookedRow] {
				receiver := q.Name
				return func(arg0 string) Assignment[hookedRow] { return InputTextSet(receiver, arg0) }
			}()).IsSet() {
				return errors.New("omitted input present")
			}
			if err := WriteSet(w, NullableInputTextSetNull(q.Note), InputTextSet(q.Name, "")); err != nil {
				return err
			}
			note, ok := WriteNullableGet(w, func() func(arg0 hookedInput) Assignment[hookedRow] {
				receiver := q.Note
				return func(arg0 hookedInput) Assignment[hookedRow] { return NullableInputTextSet(receiver, arg0) }
			}()).Get()
			if !ok || !note.IsNull() {
				return errors.New("null lost")
			}
			if err := WriteSet(w, NullableInputTextSet(q.Note, hookedInput{"input"})); err != nil {
				return err
			}
			note, _ = WriteNullableGet(w, func() func(arg0 hookedInput) Assignment[hookedRow] {
				receiver := q.Note
				return func(arg0 hookedInput) Assignment[hookedRow] { return NullableInputTextSet(receiver, arg0) }
			}()).Get()
			v, ok := note.Get()
			if !ok || v.Text != "input" {
				return errors.New("distinct input lost")
			}
			return nil
		}}
	})
	db := hookDB(t, testConnector{columns: hookColumns, values: hookValues("", "input", nil)})
	if _, err = Insert(t.Context(), db, q.Table, KeySet(q.Key(), 1), ColumnSet(q.Body, []byte{1, 2})); err != nil {
		t.Fatal(err)
	}
	if err = WriteSet(captured, InputTextSet(q.Name, "late")); !errors.Is(err, fault.Invalid) {
		t.Fatal("retained draft writable", err)
	}
}

func TestHookDepthAndDeclarationValidation(t *testing.T) {
	ctx := t.Context()
	for range lifecore.MaxDepth {
		var err error
		ctx, err = lifecore.Context(ctx)
		if err != nil {
			t.Fatal(err)
		}
	}
	q := hookedFields(func() Hooks[hookedRow] { return Hooks[hookedRow]{} })
	db := hookDB(t, testConnector{})
	if _, err := Insert(ctx, db, q.Table, KeySet(q.Key(), 1)); !errors.Is(err, fault.Invalid) {
		t.Fatal("depth", err)
	}
	bad := Define("bad", func(b Builder[hookedRow], m *hookedRow) Table[hookedRow, int64] { return TableOf(b, &m.ID) }, WithHooks[hookedRow](nil))
	if Check(bad) == nil {
		t.Fatal("nil hook factory")
	}
}

func TestEncryptedChangesUseDecodedEqualityAndRedactedDiagnostics(t *testing.T) {
	q := secureRecords()
	before := secureRecord{Secret: encrypted.NewText("private value"), Backup: value.Of(encrypted.NewText("backup private"))}
	after := before
	after.Secret = encrypted.NewText("private value")
	changes, err := Compare(q.Table, value.Set(before), value.Set(after), EncryptedSet(q.Secret, encrypted.NewText("private value")))
	if err != nil {
		t.Fatal(err)
	}
	field, err := EncryptedChangeOf(changes, q.Secret)
	if err != nil || field.Changed() || !field.Assigned() {
		t.Fatal("decoded equality", err)
	}
	nullable, err := NullableEncryptedChangeOf(changes, q.Backup)
	if err != nil || nullable.Changed() {
		t.Fatal(err)
	}
	if strings.Contains(fmt.Sprintf("%v %+v %#v", field, changes, nullable), "private") {
		t.Fatal("encrypted change leak")
	}
}

func TestDeletionObserversDispatchOnlyDeletionStages(t *testing.T) {
	q := hookedFields(nil)
	factories, calls := 0, 0
	declaration, err := NewDeletionObserver[hookedRow]("deletion.only").Declare(func() Hooks[hookedRow] {
		factories++
		return Hooks[hookedRow]{Deleting: func(context.Context, *database.Tx, hookedRow) error { calls++; return nil }}
	})
	if err != nil {
		t.Fatal(err)
	}
	db := hookDB(t, testConnector{columns: hookColumns, values: hookValues("name", nil, nil)}, declaration)
	if _, err = Insert(t.Context(), db, q.Table, KeySet(q.Key(), 1), ColumnSet(q.Body, []byte{1, 2})); err != nil {
		t.Fatal(err)
	}
	if _, err = UpdateAll(t.Context(), db, q.Query(), InputTextSet(q.Name, "name")); err != nil {
		t.Fatal(err)
	}
	if factories != 0 || calls != 0 {
		t.Fatal("deletion observer ran on save")
	}
	if _, err = Delete(t.Context(), db, q.Table.Query(), q.Table.Key(), 1); err != nil || factories != 1 || calls != 1 {
		t.Fatal(err, factories, calls)
	}
	if _, err = DeleteAll(t.Context(), db, q.Query()); !errors.Is(err, fault.Invalid) {
		t.Fatal("deletion set bypass", err)
	}
}
func TestHookOriginalMustBeUniqueAndClosedBeforeFactory(t *testing.T) {
	closeErr := errors.New("close original")
	for _, tc := range []struct {
		name  string
		rows  [][]driver.Value
		close error
		want  error
	}{
		{"missing", nil, nil, database.NotFound},
		{"ambiguous", append(hookValues("a", nil, nil), hookValues("b", nil, nil)...), nil, database.TooManyRows},
		{"close", hookValues("a", nil, nil), closeErr, closeErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			factories, closes := 0, 0
			q := hookedFields(func() Hooks[hookedRow] { factories++; return Hooks[hookedRow]{} })
			db := hookDB(t, testConnector{columns: hookColumns, values: tc.rows, closeErr: tc.close, onClose: func() { closes++ }})
			if _, err := Update(t.Context(), db, q.Table.Query(), q.Table.Key(), 1, InputTextSet(q.Name, "new")); !errors.Is(err, tc.want) {
				t.Fatal(err)
			}
			if factories != 0 || closes == 0 {
				t.Fatal("original failure reached factory or retained rows")
			}
		})
	}
}
func TestHookCancellationStopsBeforeSQL(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var statements []string
	later := false
	q := hookedFields(func() Hooks[hookedRow] {
		return Hooks[hookedRow]{Saving: func(context.Context, *database.Tx, value.Optional[hookedRow], *Write[hookedRow]) error {
			cancel()
			return nil
		}, Creating: func(context.Context, *database.Tx, *Write[hookedRow]) error { later = true; return nil }}
	})
	db := hookDB(t, testConnector{statements: &statements})
	_, err := Insert(ctx, db, q.Table, KeySet(q.Key(), 1))
	if !errors.Is(err, context.Canceled) || later || len(statements) != 0 {
		t.Fatal("cancelled hook progressed", err)
	}
}
