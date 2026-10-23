package orm

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/encrypted"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type transformRow struct {
	ID    int64
	First string
	Maybe value.Nullable[string]
	Last  string
}
type transformTable struct {
	Table[transformRow, int64]
	First Text[transformRow, string]
	Maybe NullableText[transformRow, string]
	Last  Text[transformRow, string]
}

func transformFields(first, last func(string) (string, error), maybe func(string) (string, error)) transformTable {
	return Define("transforms", func(b Builder[transformRow], m *transformRow) transformTable {
		return transformTable{TableOf(b, &m.ID), TextOf(b, &m.First, Mutate(first)), NullableTextOf(b, &m.Maybe, Mutate(maybe)), TextOf(b, &m.Last, Mutate(last))}
	})
}
func TestMutatorsPreservePresenceOrderAndStoredReads(t *testing.T) {
	var order []string
	begun := false
	transform := func(name string) func(string) (string, error) {
		return func(v string) (string, error) {
			if !begun {
				t.Error("transform outside transaction")
			}
			order = append(order, name)
			return strings.TrimSpace(v), nil
		}
	}
	fields := transformFields(transform("first"), transform("last"), transform("maybe"))
	if err := Check(fields); err != nil {
		t.Fatal(err)
	}
	var bindings [][]any
	db := testDB(t, testConnector{bindings: &bindings, onBegin: func() { begun = true }, columns: []string{"id", "first", "maybe", "last"}, values: [][]driver.Value{{int64(1), "stored", nil, "last"}}})
	a := fields.First.Set(" input ")
	if len(order) != 0 {
		t.Fatal("Set invoked transform")
	}
	for range 2 {
		order = nil
		begun = false
		got, err := fields.Insert(t.Context(), db, fields.Key().Set(1), fields.Last.Set(" last "), fields.Maybe.SetNull(), a)
		if err != nil || got.First != "stored" {
			t.Fatal("stored hydration", err)
		}
		if !reflect.DeepEqual(order, []string{"first", "last"}) {
			t.Fatal("declaration/null order", order)
		}
	}
	if !reflect.DeepEqual(bindings[0], []any{int64(1), "input", nil, "last"}) || !reflect.DeepEqual(bindings[0], bindings[1]) {
		t.Fatal("input reuse/binding", bindings)
	}
	order = nil
	_, err := fields.Require(t.Context(), db, 1)
	if err != nil || len(order) != 0 {
		t.Fatal("read invoked transform", err)
	}
	_, err = fields.Update(t.Context(), db, 1, fields.Maybe.Set(""))
	if err != nil || !reflect.DeepEqual(order, []string{"maybe"}) {
		t.Fatal("zero value skipped", err, order)
	}
}
func TestMutatorFaultsAbortBeforeSQL(t *testing.T) {
	veto := errors.New("veto")
	for name, fn := range map[string]func(string) (string, error){
		"error":  func(string) (string, error) { return "", veto },
		"panic":  func(string) (string, error) { panic("private") },
		"goexit": func(string) (string, error) { runtime.Goexit(); return "", nil },
	} {
		t.Run(name, func(t *testing.T) {
			identity := func(v string) (string, error) { return v, nil }
			fields := transformFields(fn, identity, identity)
			var statements []string
			begun := false
			db := testDB(t, testConnector{statements: &statements, onBegin: func() { begun = true }})
			_, err := fields.Insert(t.Context(), db, fields.Key().Set(1), fields.First.Set("private"), fields.Last.Set("last"))
			if err == nil || !begun || len(statements) != 0 {
				t.Fatal("failure escaped transaction or reached SQL", err)
			}
			if name == "error" && !errors.Is(err, veto) {
				t.Fatal("error identity lost", err)
			}
			if name != "error" && !errors.Is(err, fault.Panicked) {
				t.Fatal("callback isolation lost", err)
			}
		})
	}
}
func TestMutatorCancellationAndBatchFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	first := func(v string) (string, error) { calls++; cancel(); return v, nil }
	last := func(v string) (string, error) { t.Error("callback after cancellation"); return v, nil }
	fields := transformFields(first, last, last)
	var statements []string
	db := testDB(t, testConnector{statements: &statements})
	_, err := fields.Insert(ctx, db, fields.Key().Set(1), fields.First.Set("x"), fields.Last.Set("y"))
	if !errors.Is(err, context.Canceled) || calls != 1 || len(statements) != 0 {
		t.Fatal("cancellation", err, calls)
	}
	veto := errors.New("second row")
	fields = transformFields(func(v string) (string, error) {
		if v == "reject" {
			return "", veto
		}
		return v, nil
	}, func(v string) (string, error) { return v, nil }, nil)
	// This declaration intentionally proves nil transforms fail at startup.
	if Check(fields) == nil {
		t.Fatal("nil option accepted")
	}
	fields = transformFields(func(v string) (string, error) {
		if v == "reject" {
			return "", veto
		}
		return v, nil
	}, func(v string) (string, error) { return v, nil }, func(v string) (string, error) { return v, nil })
	_, err = fields.InsertMany(t.Context(), db, [][]Assignment[transformRow]{
		{fields.Key().Set(1), fields.First.Set("x"), fields.Last.Set("y")},
		{fields.Key().Set(2), fields.First.Set("reject"), fields.Last.Set("y")},
	})
	if !errors.Is(err, veto) || len(statements) != 0 {
		t.Fatal("batch failure reached SQL", err)
	}
}

type orphanMutator struct {
	ID    int64
	Email string
}

func (orphanMutator) MutateEamil(v string) (string, error) { return v, nil }

type pointerMutator struct {
	ID    int64
	Email string
}

func (*pointerMutator) MutateEmail(v string) (string, error) { return v, nil }

type invalidAccessor struct {
	ID    int64
	Email string
}

func (invalidAccessor) AccessEmail() (any, error) { return nil, nil }

type orphanAccessor struct {
	ID    int64
	Email string
}

func (orphanAccessor) AccessMissing() (string, error) { return "", nil }

type namedMutator struct {
	ID    int64
	Email string
}

func (namedMutator) MutateEmail(v string) (string, error) { return v, nil }

type distinctMutator struct {
	ID    int64
	Email string
}

func (distinctMutator) MutateEmail(v int) (string, error) { return "", nil }

type encryptedMutator struct {
	ID     int64
	Secret encrypted.Text
}

func (encryptedMutator) MutateSecret(v encrypted.Text) (encrypted.Text, error) { return v, nil }

type explicitInputModel struct {
	ID   int64
	Name string
}

func TestExplicitMutatorInputMustBeConcreteAndNonNull(t *testing.T) {
	tables := []CheckedTable{
		Define("interface_input", func(b Builder[explicitInputModel], m *explicitInputModel) Table[explicitInputModel, int64] {
			InputTextOf(b, &m.Name, func(any) (string, error) { return "", nil })
			return TableOf(b, &m.ID)
		}),
		Define("nullable_input", func(b Builder[explicitInputModel], m *explicitInputModel) Table[explicitInputModel, int64] {
			InputTextOf(b, &m.Name, func(value.Nullable[string]) (string, error) { return "", nil })
			return TableOf(b, &m.ID)
		}),
	}
	for _, table := range tables {
		if Check(table) == nil {
			t.Fatal("invalid explicit mutator input accepted")
		}
	}
}

func TestFieldMethodStartupRejectsInvalidDeclarations(t *testing.T) {
	declarations := []CheckedTable{
		Define("orphan", func(b Builder[orphanMutator], m *orphanMutator) Table[orphanMutator, int64] {
			TextOf(b, &m.Email)
			return TableOf(b, &m.ID)
		}),
		Define("pointer", func(b Builder[pointerMutator], m *pointerMutator) Table[pointerMutator, int64] {
			TextOf(b, &m.Email)
			return TableOf(b, &m.ID)
		}),
		Define("accessor", func(b Builder[invalidAccessor], m *invalidAccessor) Table[invalidAccessor, int64] {
			TextOf(b, &m.Email)
			return TableOf(b, &m.ID)
		}),
		Define("orphan_accessor", func(b Builder[orphanAccessor], m *orphanAccessor) Table[orphanAccessor, int64] {
			TextOf(b, &m.Email)
			return TableOf(b, &m.ID)
		}),
		Define("conflict", func(b Builder[namedMutator], m *namedMutator) Table[namedMutator, int64] {
			TextOf(b, &m.Email, Mutate(func(v string) (string, error) { return v, nil }))
			return TableOf(b, &m.ID)
		}),
		Define("distinct", func(b Builder[distinctMutator], m *distinctMutator) Table[distinctMutator, int64] {
			TextOf(b, &m.Email)
			return TableOf(b, &m.ID)
		}),
		Define("missing", func(b Builder[orphanMutator], m *orphanMutator) Table[orphanMutator, int64] {
			InputTextOf[orphanMutator, int](b, &m.Email, nil)
			return TableOf(b, &m.ID)
		}),
		Define("encrypted", func(b Builder[encryptedMutator], m *encryptedMutator) Table[encryptedMutator, int64] {
			EncryptedTextOf(b, &m.Secret)
			return TableOf(b, &m.ID)
		}),
	}
	for _, declaration := range declarations {
		if err := Check(declaration); err == nil {
			t.Fatal("invalid field method accepted")
		}
	}
}

type nativeMethodRow struct {
	ID   int64
	Name string
}

func (nativeMethodRow) MutateName(v string) (string, error) {
	pcs := make([]uintptr, 32)
	n := runtime.Callers(1, pcs)
	frames := runtime.CallersFrames(pcs[:n])
	for {
		frame, more := frames.Next()
		if strings.HasPrefix(frame.Function, "reflect.") {
			return "", errors.New("reflective mutator invocation")
		}
		if !more {
			break
		}
	}
	return v, nil
}
func TestNamedMutatorsUseNativeTypedCalls(t *testing.T) {
	fields := Define("native_methods", func(b Builder[nativeMethodRow], m *nativeMethodRow) Table[nativeMethodRow, int64] {
		TextOf(b, &m.Name)
		return TableOf(b, &m.ID)
	})
	if err := Check(fields); err != nil {
		t.Fatal(err)
	}
	db := testDB(t, testConnector{columns: []string{"id", "name"}, values: [][]driver.Value{{int64(1), "stored"}}})
	_, err := fields.Insert(t.Context(), db, fields.Key().Set(1), columnAssignment[nativeMethodRow](columnWithCodec(fields.core.columns[1], scalarCodec[string]()), "input"))
	if err != nil {
		t.Fatal(err)
	}
}

type foreignInputOwner struct {
	ID     int64
	Target model.ID[nativeMethodRow]
}
type targetInput struct{ ID model.ID[nativeMethodRow] }

func (foreignInputOwner) MutateTarget(v targetInput) (model.ID[nativeMethodRow], error) {
	return v.ID, nil
}
func TestDistinctForeignKeyInputIsNotAnAutomaticPrimaryKey(t *testing.T) {
	fields := Define("foreign_inputs", func(b Builder[foreignInputOwner], m *foreignInputOwner) Table[foreignInputOwner, int64] {
		InputColumnOf[foreignInputOwner, targetInput](b, &m.Target, nil)
		return TableOf(b, &m.ID)
	})
	if err := Check(fields); err != nil {
		t.Fatal(err)
	}
}
func TestNumericAssignmentCannotBypassMutator(t *testing.T) {
	type row struct {
		ID     int64
		Amount int64
	}
	type fields struct {
		Table[row, int64]
		Amount Number[row, int64]
	}
	table := Define("numeric_mutators", func(b Builder[row], m *row) fields {
		return fields{TableOf(b, &m.ID), NumberOf(b, &m.Amount, Mutate(func(v int64) (int64, error) { t.Error("SQL adjustment invoked scalar mutator"); return v, nil }))}
	})
	if err := Check(table); err != nil {
		t.Fatal(err)
	}
	var statements []string
	db := testDB(t, testConnector{statements: &statements})
	_, err := table.Update(t.Context(), db, 1, table.Amount.Increment(1))
	if !errors.Is(err, fault.Invalid) || len(statements) != 0 {
		t.Fatal("numeric mutator bypass", err)
	}
}
