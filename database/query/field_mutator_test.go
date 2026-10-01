package query

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

type mutatorRecord struct {
	id   int
	name string
	note value.Nullable[string]
}

func mutatorQuery(name, note func(string) (string, error)) Query[mutatorRecord] {
	return ForModel(Define("records", "id", []Column{{Name: "id", DatabaseDefault: true}, {Name: "name"}, {Name: "note", Nullable: true}}, func(database.Row) (mutatorRecord, error) {
		return mutatorRecord{}, nil
	}, NewMutatedModelField("name", codec.String[string](), func(r mutatorRecord) string { return r.name }, name),
		NewNullableMutatedModelField("note", codec.String[string](), func(r mutatorRecord) value.Nullable[string] { return r.note }, note)))
}

func TestFieldMutatorsPreserveAssignmentStateAndOrder(t *testing.T) {
	var calls []string
	q := mutatorQuery(func(s string) (string, error) {
		calls = append(calls, "name")
		return s + "!", nil
	}, func(s string) (string, error) {
		calls = append(calls, "note")
		return s + "?", nil
	})
	name := Assign[mutatorRecord]("records", "name", codec.String[string](), "")
	note := Assign[mutatorRecord]("records", "note", codec.Nullable(codec.String[string]()), value.Of(""))
	input := Change(note, name) // Setter order differs from declaration order.
	plan := insertPlan[mutatorRecord]{query: q, rows: []Mutation[mutatorRecord]{input}}
	for range 2 {
		calls = nil
		statement, err := plan.prepare(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(statement.Arguments(), []any{"!", "?"}) || !reflect.DeepEqual(calls, []string{"name", "note"}) {
			t.Fatalf("mutators lost values, order or once-per-write behavior: %v %v", statement.Arguments(), calls)
		}
	}
	if got, _ := input.assignments[1].bind(); got != "" {
		t.Fatal("mutation preparation changed the caller's input")
	}
	for _, mutation := range []Mutation[mutatorRecord]{
		Change(name),
		Change(name, Assign[mutatorRecord]("records", "note", codec.Nullable(codec.String[string]()), value.Null[string]())),
	} {
		calls = nil
		statement, err := (insertPlan[mutatorRecord]{query: q, rows: []Mutation[mutatorRecord]{mutation}}).prepare(t.Context(), nil)
		if err != nil || !reflect.DeepEqual(calls, []string{"name"}) {
			t.Fatalf("omitted/NULL field invoked its scalar mutator: %v %v", calls, err)
		}
		if len(mutation.assignments) == 2 && statement.Arguments()[1] != nil {
			t.Fatal("explicit NULL changed into a scalar zero")
		}
	}
}

func TestFieldMutatorFailureRunsInsideTransaction(t *testing.T) {
	sentinel := errors.New("mutator veto")
	active, calls, begins := false, 0, 0
	q := mutatorQuery(func(s string) (string, error) {
		calls++
		if !active {
			t.Error("mutator ran outside the write transaction")
		}
		return "", sentinel
	}, func(s string) (string, error) { return s, nil })
	writer := mutatorTransactor(func(ctx context.Context, work func(*database.Tx) error) error {
		begins++
		active = true
		defer func() { active = false }()
		return work(nil) // The veto must prevent any connection use.
	})
	mutation := Change(Assign[mutatorRecord]("records", "name", codec.String[string](), "input"))
	if _, err := q.Insert(t.Context(), writer, mutation); !errors.Is(err, sentinel) {
		t.Fatalf("mutator error was lost: %v", err)
	}
	id := NewScalarField[mutatorRecord, int]("records", "id", codec.Signed[int]())
	if _, err := q.Where(id.Eq(1)).Patch(t.Context(), writer, mutation); !errors.Is(err, sentinel) {
		t.Fatalf("patch mutator error was lost: %v", err)
	}
	if _, err := q.InsertMany(t.Context(), writer, []Mutation[mutatorRecord]{mutation}); !errors.Is(err, sentinel) {
		t.Fatalf("batch mutator error was lost: %v", err)
	}
	if _, err := q.InsertOnConflict(t.Context(), writer, mutation, OnConflict[mutatorRecord]().DoNothing()); !errors.Is(err, sentinel) {
		t.Fatalf("upsert mutator error was lost: %v", err)
	}
	if calls != 4 || begins != 4 {
		t.Fatalf("write retried or missed its mutator: calls=%d begins=%d", calls, begins)
	}
	if _, err := q.InsertMany(t.Context(), writer, nil); err != nil || begins != 4 {
		t.Fatalf("valid empty batch started a transaction: %v", err)
	}
}

type mutatorTransactor func(context.Context, func(*database.Tx) error) error

func (w mutatorTransactor) Transaction(ctx context.Context, work func(*database.Tx) error, _ ...database.TxOptions) error {
	return w(ctx, work)
}

func TestFieldMutatorsValidateOutputAndHonorCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	noteCalls := 0
	q := mutatorQuery(func(s string) (string, error) { cancel(); return s, nil }, func(s string) (string, error) {
		noteCalls++
		return s, nil
	})
	mutation := Change(Assign[mutatorRecord]("records", "name", codec.String[string](), "input"), Assign[mutatorRecord]("records", "note", codec.Nullable(codec.String[string]()), value.Of("note")))
	if _, err := (insertPlan[mutatorRecord]{query: q, rows: []Mutation[mutatorRecord]{mutation}}).prepare(ctx, nil); !errors.Is(err, context.Canceled) || noteCalls != 0 {
		t.Fatalf("cancellation did not stop remaining mutators: %v", err)
	}
	invalid := errors.New("invalid normalized value")
	c := codec.String[string]().Validated(func(s string) error {
		if s != "valid" {
			return invalid
		}
		return nil
	})
	for _, output := range []string{"valid", "invalid"} {
		q.definition.modelFields[0] = NewMutatedModelField("name", c, func(r mutatorRecord) string { return r.name }, func(string) (string, error) { return output, nil })
		// Invalid original inputs can normalize into valid values; only the
		// transformed value goes through the final declared database codec.
		input := Change(Assign[mutatorRecord]("records", "name", c, "unvalidated input"))
		_, err := (insertPlan[mutatorRecord]{query: q, rows: []Mutation[mutatorRecord]{input}}).prepare(t.Context(), nil)
		if (output == "valid" && err != nil) || (output == "invalid" && !errors.Is(err, invalid)) {
			t.Fatalf("final validation used the wrong value: %v", err)
		}
	}
}

func TestFieldMutatorsRejectInvalidDeclarationsAndInputs(t *testing.T) {
	calls := 0
	transform := func(s string) (string, error) { calls++; return s, nil }
	q := mutatorQuery(transform, transform)
	name := Assign[mutatorRecord]("records", "name", codec.String[string](), "private-input")
	for _, mutation := range []Mutation[mutatorRecord]{
		Change(name, name),
		Change(Assign[mutatorRecord]("wrong", "name", codec.String[string](), "x")),
		Change(Assign[mutatorRecord]("records", "name", codec.Signed[int](), 1)),
		Change(name, Assign[mutatorRecord]("records", "note", codec.Signed[int](), 1)),
	} {
		if _, err := (insertPlan[mutatorRecord]{query: q, rows: []Mutation[mutatorRecord]{mutation}}).prepare(t.Context(), nil); !errors.Is(err, fault.Invalid) {
			t.Fatalf("invalid assignment was transformed: %v", err)
		}
	}
	if calls != 0 {
		t.Fatal("invalid mutation invoked user code")
	}
	for _, q := range []Query[mutatorRecord]{mutatorQuery(nil, transform), mutatorQuery(transform, nil)} {
		if err := q.Validate(); !errors.Is(err, fault.Invalid) {
			t.Fatalf("nil mutator silently disabled field behavior: %v", err)
		}
	}
	for _, diagnostic := range []string{fmt.Sprint(name), fmt.Sprintf("%#v", name), fmt.Sprintf("%+v", Change(name)), fmt.Sprintf("%#v", Change(name))} {
		if strings.Contains(diagnostic, "private-input") {
			t.Fatal("pending field value leaked into mutation diagnostics")
		}
	}
}
