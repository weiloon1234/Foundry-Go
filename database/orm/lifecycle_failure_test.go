package orm

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

type privateLifecycleError struct{ modelValue string }

func (e *privateLifecycleError) Error() string { return "rejected model value: " + e.modelValue }
func requireSafeLifecycleError(t *testing.T, err error, cause *privateLifecycleError) {
	t.Helper()
	if !errors.Is(err, cause) {
		t.Fatal("error identity lost")
	}
	var got *privateLifecycleError
	if !errors.As(err, &got) || got != cause {
		t.Fatal("typed cause lost")
	}
	for _, formatted := range []string{err.Error(), fmt.Sprintf("%v", err), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err)} {
		if strings.Contains(formatted, cause.modelValue) {
			t.Fatal("model value leaked in error formatting")
		}
	}
}
func TestLifecycleHookErrorsRedactModelValues(t *testing.T) {
	const modelValue = "private-lifecycle-model-value"
	for _, stage := range []string{"before", "after", "after_commit"} {
		for _, source := range []string{"local", "observer"} {
			t.Run(stage+"/"+source, func(t *testing.T) {
				cause := &privateLifecycleError{modelValue: modelValue}
				factory := func() Hooks[hookedRow] {
					h := Hooks[hookedRow]{}
					switch stage {
					case "before":
						h.Creating = func(context.Context, *database.Tx, *Write[hookedRow]) error { return cause }
					case "after":
						h.Created = func(context.Context, *database.Tx, Changes[hookedRow]) error { return cause }
					case "after_commit":
						h.AfterCommit = func(context.Context, lifecycle.Operation, Changes[hookedRow]) error { return cause }
					}
					return h
				}
				var q hookedTable
				var declarations []lifecycle.Declaration
				if source == "local" {
					q = hookedFields(factory)
				} else {
					q = hookedFields(nil)
					declaration, err := NewObserver[hookedRow]("redaction").Declare(factory)
					if err != nil {
						t.Fatal(err)
					}
					declarations = append(declarations, declaration)
				}
				db := hookDB(t, testConnector{columns: hookColumns, values: hookValues(modelValue, nil, nil)}, declarations...)
				got, err := Insert(t.Context(), db, q.Table, KeySet(q.Key(), 1), InputTextSet(q.Name, modelValue), ColumnSet(q.Body, []byte{1, 2}))
				requireSafeLifecycleError(t, err, cause)
				if got.ID != 0 {
					t.Fatal("failed hook published a model")
				}
			})
		}
	}
}
func TestORMComparisonErrorsRedactOriginalAndResultingValues(t *testing.T) {
	type row struct {
		ID   int64
		Name string
	}
	type table struct {
		Table[row, int64]
		Name Text[row, string]
	}
	const modelValue = "private-comparison-model-value"
	cause := &privateLifecycleError{modelValue: modelValue}
	reject := true
	c := codec.New(func(v string) (driver.Value, error) {
		if reject && v == modelValue {
			return nil, cause
		}
		return v, nil
	}, func(raw any) (string, error) { return raw.(string), nil })
	q := Define("comparison_redaction", func(b Builder[row], m *row) table { return table{TableOf(b, &m.ID), TextOf(b, &m.Name, Codec(c))} })
	for _, side := range []string{"original", "resulting"} {
		t.Run(side, func(t *testing.T) {
			before, after := row{ID: 1, Name: "ordinary"}, row{ID: 1, Name: "ordinary"}
			if side == "original" {
				before.Name = modelValue
			} else {
				after.Name = modelValue
			}
			reject = true
			_, err := Compare(q.Table, value.Set(before), value.Set(after))
			requireSafeLifecycleError(t, err, cause)
			reject = false
			changes, err := Compare(q.Table, value.Set(before), value.Set(after))
			if err != nil {
				t.Fatal(err)
			}
			reject = true
			_, err = ChangeOf(changes, TextExpr(q.Name))
			requireSafeLifecycleError(t, err, cause)
		})
	}
}
func TestWriteChangeCaptureErrorsAreRedactedAndAbortBeforeAfterHooks(t *testing.T) {
	type row struct {
		ID   int64
		Name string
	}
	type table struct {
		Table[row, int64]
		Name Text[row, string]
	}
	const modelValue = "private-returning-model-value"
	cause := &privateLifecycleError{modelValue: modelValue}
	afterCalled := false
	c := codec.New(func(v string) (driver.Value, error) {
		if v == modelValue {
			return nil, cause
		}
		return v, nil
	}, func(raw any) (string, error) { return raw.(string), nil })
	q := Define("capture_redaction", func(b Builder[row], m *row) table { return table{TableOf(b, &m.ID), TextOf(b, &m.Name, Codec(c))} }, WithHooks(func() Hooks[row] {
		return Hooks[row]{Created: func(context.Context, *database.Tx, Changes[row]) error { afterCalled = true; return nil }}
	}))
	db := hookDB(t, testConnector{columns: []string{"id", "name"}, values: [][]driver.Value{{int64(1), modelValue}}})
	got, err := Insert(t.Context(), db, q.Table, KeySet(q.Key(), 1), TextSet(q.Name, "ordinary"))
	requireSafeLifecycleError(t, err, cause)
	if got.ID != 0 || afterCalled {
		t.Fatal("failed change capture published state or ran after hooks")
	}
}
func TestHookModifiedPrimaryKeyRejectedBeforeMutatorsAndMutationSQL(t *testing.T) {
	type row struct {
		ID        int64
		Name      string
		CreatedAt time.Time
		UpdatedAt time.Time
	}
	type table struct {
		Table[row, int64]
		Name InputText[row, string, string]
	}
	mutatorCalls := 0
	var q table
	q = Define("hook_shape", func(b Builder[row], m *row) table {
		CreatedAtOf(b, &m.CreatedAt)
		UpdatedAtOf(b, &m.UpdatedAt)
		return table{TableOf(b, &m.ID), InputTextOf(b, &m.Name, func(v string) (string, error) { mutatorCalls++; return v, nil })}
	}, WithHooks(func() Hooks[row] {
		return Hooks[row]{Updating: func(ctx context.Context, tx *database.Tx, before row, w *Write[row]) error {
			return WriteSet(w, KeySet(q.Key(), 2), InputTextSet(q.Name, "hook value"))
		}}
	}))
	now := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	var statements []string
	db := hookDB(t, testConnector{statements: &statements, columns: []string{"id", "name", "created_at", "updated_at"}, values: [][]driver.Value{{int64(1), "original", now, now}}})
	got, err := Update(t.Context(), db, q.Table.Query(), q.Table.Key(), 1, InputTextSet(q.Name, "input"))
	if !errors.Is(err, fault.Invalid) || got.ID != 0 {
		t.Fatal("invalid effective draft accepted", err)
	}
	if mutatorCalls != 0 {
		t.Fatal("invalid draft reached a mutator")
	}
	// Updating hooks receive a locked original. No mutation SQL may follow it.
	if len(statements) != 1 || !strings.HasPrefix(statements[0], "SELECT ") || !strings.Contains(statements[0], "FOR UPDATE") {
		t.Fatal("invalid draft sent mutation SQL", statements)
	}
}
