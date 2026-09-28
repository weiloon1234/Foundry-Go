package query

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// These fixture values deliberately have distinct representations. The input
// is a concrete struct with no SQL codec; normalization returns stored text.
type inputName struct{ supplied string }
type storedName string
type inputRecord struct {
	ID   int
	Name storedName
	Note value.Nullable[storedName]
}

func inputQuery(calls *[]string) Query[inputRecord] {
	return ForModel(Define("input_records", "id", []Column{{Name: "id"}, {Name: "name"}, {Name: "note", Nullable: true}},
		func(database.Row) (inputRecord, error) { return inputRecord{}, nil },
		NewModelField("id", codec.Signed[int](), func(r inputRecord) int { return r.ID }),
		NewInputModelField("name", codec.String[storedName](), func(r inputRecord) storedName { return r.Name }, func(input inputName) (storedName, error) {
			*calls = append(*calls, "name")
			return storedName(strings.ToLower(strings.TrimSpace(input.supplied))), nil
		}),
		NewNullableInputModelField("note", codec.String[storedName](), func(r inputRecord) value.Nullable[storedName] { return r.Note }, func(input inputName) (storedName, error) {
			*calls = append(*calls, "note")
			return storedName(strings.TrimSpace(input.supplied)), nil
		}),
	))
}

func TestMutationInputTransformsBeforeBindingAndRemainsReusable(t *testing.T) {
	var calls []string
	q := inputQuery(&calls)
	name := AssignInput[inputRecord]("input_records", "name", inputName{" ADA "})
	mutation := Change(Assign[inputRecord]("input_records", "id", codec.Signed[int](), 1), name,
		AssignInput[inputRecord]("input_records", "note", value.Of(inputName{" memo "})))
	if _, err := name.bind(); !errors.Is(err, fault.Invalid) {
		t.Fatal("untransformed input reached binding", err)
	}
	plain := *q.definition
	plain.modelFields = slices.Clone(plain.modelFields)
	for i := range plain.modelFields {
		plain.modelFields[i].mutator = fieldMutator{}
		plain.modelFields[i].conflictMutator = fieldMutator{}
	}
	if _, err := (insertPlan[inputRecord]{query: ForModel(plain), rows: []Mutation[inputRecord]{mutation}}).prepare(t.Context()); !errors.Is(err, fault.Invalid) || len(calls) != 0 {
		t.Fatal("pending input compiled without its transformation", err)
	}
	if strings.Contains(fmt.Sprintf("%+v %#v", name, mutation), "ADA") {
		t.Fatal("pending input leaked through mutation diagnostics")
	}
	for range 2 {
		calls = nil
		statement, err := (insertPlan[inputRecord]{query: q, rows: []Mutation[inputRecord]{mutation}}).prepare(t.Context())
		if err != nil || !reflect.DeepEqual(statement.Arguments(), []any{int64(1), "ada", "memo"}) || !reflect.DeepEqual(calls, []string{"name", "note"}) {
			t.Fatal("distinct input was lost or transformed twice", statement.Arguments(), calls, err)
		}
	}
	values, err := ReadMutation(mutation)
	if err != nil {
		t.Fatal(err)
	}
	input, err := MutationValue[inputRecord, inputName](values, "input_records", "name")
	got, _ := input.Get()
	if err != nil || got.supplied != " ADA " {
		t.Fatal("before-hook input no longer has its original type/value", err)
	}
	if _, err := MutationValue[inputRecord, storedName](values, "input_records", "name"); !errors.Is(err, fault.Invalid) {
		t.Fatal("input was implicitly converted to storage")
	}
}

func TestMutationInputPreservesNullOmissionAndRejectsStoredValues(t *testing.T) {
	var calls []string
	q := inputQuery(&calls)
	for _, includeNull := range []bool{false, true} {
		mutation := Change(Assign[inputRecord]("input_records", "id", codec.Signed[int](), 1), AssignInput[inputRecord]("input_records", "name", inputName{}))
		if includeNull {
			mutation.assignments = append(mutation.assignments, AssignInput[inputRecord]("input_records", "note", value.Null[inputName]()))
		}
		calls = nil
		statement, err := (insertPlan[inputRecord]{query: q, rows: []Mutation[inputRecord]{mutation}}).prepare(t.Context())
		if err != nil || !reflect.DeepEqual(calls, []string{"name"}) {
			t.Fatal("nullable input invoked a scalar transformation", calls, err)
		}
		if includeNull && statement.Arguments()[2] != nil {
			t.Fatal("explicit input NULL changed")
		}
	}
	calls = nil
	wrong := Change(Assign[inputRecord]("input_records", "name", codec.String[storedName](), storedName("already stored")))
	if _, err := q.mutateFields(t.Context(), wrong); !errors.Is(err, fault.Invalid) || len(calls) != 0 {
		t.Fatal("stored value accepted as fresh input", calls, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := q.mutateFields(ctx, Change(AssignInput[inputRecord]("input_records", "name", inputName{}))); !errors.Is(err, context.Canceled) || len(calls) != 0 {
		t.Fatal("canceled input transformation ran", calls, err)
	}
}

func TestMutationInputConflictUsesFreshInputAndCopiesProposedOnce(t *testing.T) {
	var calls []string
	q := inputQuery(&calls)
	name := NewTextField[inputRecord]("input_records", "name", codec.String[storedName]())
	input := NewMutationInputField[inputRecord, inputName]("input_records", "name")
	mutation := Change(Assign[inputRecord]("input_records", "id", codec.Signed[int](), 1), AssignInput[inputRecord]("input_records", "name", inputName{" INSERTED "}))
	for _, literal := range []bool{false, true} {
		update := name.Incoming()
		if literal {
			update = input.Set(inputName{" UPDATED "})
		}
		policy := OnConflict(name).DoUpdate(update)
		for range 2 {
			calls = nil
			statement, err := (insertPlan[inputRecord]{query: q, rows: []Mutation[inputRecord]{mutation}, conflict: &policy}).prepare(t.Context())
			wantCalls := 1
			if literal {
				wantCalls = 2
			}
			if err != nil || len(calls) != wantCalls {
				t.Fatal("conflict input transformed incorrectly", calls, err)
			}
			if literal && !reflect.DeepEqual(statement.Arguments(), []any{int64(1), "inserted", "updated"}) {
				t.Fatal("conflict literal did not bind stored output", statement.Arguments())
			}
		}
	}
	if err := name.Set(storedName("stored")).validateMutator(q); !errors.Is(err, fault.Invalid) {
		t.Fatal("low-level stored conflict literal bypassed input type")
	}
}
