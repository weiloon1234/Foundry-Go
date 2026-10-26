package orm

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
	"reflect"
	"strings"
	"testing"
)

type transferInput struct{ Text string }
type transferStored string
type transferMutated struct {
	ID   int64 `foundry:"default=database"`
	Name transferStored
	Note value.Nullable[transferStored]
}
type transferMutators struct {
	Table[transferMutated, int64]
	Name InputText[transferMutated, transferInput, transferStored]
	Note NullableInputText[transferMutated, transferInput, transferStored]
}

func transferMutatorFields(name, note func(transferInput) (transferStored, error), opts ...ColumnOption[transferStored]) transferMutators {
	return Define("input_records", func(b Builder[transferMutated], m *transferMutated) transferMutators {
		return transferMutators{TableOf(b, &m.ID), InputTextOf(b, &m.Name, name, opts...), NullableInputTextOf(b, &m.Note, note)}
	})
}
func TestMutationInputTransformsBeforeBindingAndRemainsReusable(t *testing.T) {
	var calls []string
	d := transferMutatorFields(func(v transferInput) (transferStored, error) {
		calls = append(calls, "name")
		return transferStored(strings.ToLower(strings.TrimSpace(v.Text))), nil
	}, func(v transferInput) (transferStored, error) {
		calls = append(calls, "note")
		return transferStored(strings.TrimSpace(v.Text)), nil
	})
	name := d.Name.Set(transferInput{" ADA "})
	assignments := []Assignment[transferMutated]{d.Key().Set(1), name, d.Note.Set(transferInput{" memo "})}
	if _, e := name.core.Bind(); !errors.Is(e, fault.Invalid) {
		t.Fatal("untransformed binding", e)
	}
	p, e := planWrite(d.Query().state, querycore.InsertModel, assignments, false)
	if e != nil {
		t.Fatal(e)
	}
	plain := p
	core := *p.query.table
	core.mutators = nil
	plain.query.table = &core
	unchanged, e := plain.mutate(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	if _, e := unchanged.compile(); !errors.Is(e, fault.Invalid) || len(calls) != 0 {
		t.Fatal("missing transformation", e)
	}
	for range 2 {
		calls = nil
		prepared, e := p.mutate(t.Context())
		if e != nil {
			t.Fatal(e)
		}
		s, e := prepared.compile()
		if e != nil || !reflect.DeepEqual(s.Bindings, []any{int64(1), "ada", "memo"}) || !reflect.DeepEqual(calls, []string{"name", "note"}) {
			t.Fatal(s.Bindings, calls, e)
		}
	}
	draft := &Write[transferMutated]{state: &draftState{table: d.core, assignments: append([]querycore.MutationAssignment(nil), p.assignments...), open: true}}
	got, ok := WriteGet(draft, d.Name.Set).Get()
	if !ok || got.Text != " ADA " {
		t.Fatal("input type/value lost")
	}
	wrong := func(v transferStored) Assignment[transferMutated] {
		a := d.Name.Set(transferInput{})
		a.core.AssignmentValue = querycore.CaptureAssignment(codec.String[transferStored](), v)
		return a
	}
	if WriteGet(draft, wrong).IsSet() || !errors.Is(draft.state.err, fault.Invalid) {
		t.Fatal("coerced input to storage")
	}
	for _, v := range []any{name, draft, p.assignments} {
		if strings.Contains(fmt.Sprintf("%v %+v %#v", v, v, v), "ADA") {
			t.Fatal("pending input disclosed")
		}
	}
}
func TestMutationInputPreservesNullOmissionAndRejectsStoredValues(t *testing.T) {
	calls := 0
	fn := func(v transferInput) (transferStored, error) { calls++; return transferStored(v.Text), nil }
	d := transferMutatorFields(fn, fn)
	for _, null := range []bool{false, true} {
		as := []Assignment[transferMutated]{d.Key().Set(1), d.Name.Set(transferInput{})}
		if null {
			as = append(as, d.Note.SetNull())
		}
		calls = 0
		p, e := planWrite(d.Query().state, querycore.InsertModel, as, false)
		if e != nil {
			t.Fatal(e)
		}
		p, e = p.mutate(t.Context())
		if e != nil {
			t.Fatal(e)
		}
		s, e := p.compile()
		if e != nil || calls != 1 || null && s.Bindings[2] != nil {
			t.Fatal("NULL/omission transformed", calls, s.Bindings, e)
		}
	}
	calls = 0
	wrong := d.Name.Set(transferInput{})
	wrong.core.AssignmentValue = querycore.CaptureAssignment(codec.String[transferStored](), transferStored("already stored"))
	p, e := planWrite(d.Query().state, querycore.InsertModel, []Assignment[transferMutated]{wrong}, false)
	if e == nil {
		_, e = p.mutate(t.Context())
	}
	if !errors.Is(e, fault.Invalid) || calls != 0 {
		t.Fatal("stored input bypass", e)
	}
	p, e = planWrite(d.Query().state, querycore.InsertModel, []Assignment[transferMutated]{d.Name.Set(transferInput{})}, false)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, e := p.mutate(ctx); !errors.Is(e, context.Canceled) || calls != 0 {
		t.Fatal("canceled transform", e)
	}
}
func TestFieldMutatorsPreserveAssignmentStateAndOrder(t *testing.T) {
	var calls []string
	d := transferMutatorFields(func(v transferInput) (transferStored, error) {
		calls = append(calls, "name")
		return transferStored(v.Text + "!"), nil
	}, func(v transferInput) (transferStored, error) {
		calls = append(calls, "note")
		return transferStored(v.Text + "?"), nil
	})
	input := []Assignment[transferMutated]{d.Note.Set(transferInput{}), d.Name.Set(transferInput{})}
	p, e := planWrite(d.Query().state, querycore.InsertModel, input, false)
	if e != nil {
		t.Fatal(e)
	}
	for range 2 {
		calls = nil
		prepared, e := p.mutate(t.Context())
		if e != nil {
			t.Fatal(e)
		}
		s, e := prepared.compile()
		if e != nil || !reflect.DeepEqual(s.Bindings, []any{"!", "?"}) || !reflect.DeepEqual(calls, []string{"name", "note"}) {
			t.Fatal(s.Bindings, calls, e)
		}
	}
	if input[1].core.InputValue().(transferInput).Text != "" {
		t.Fatal("caller input mutated")
	}
}
func TestFieldMutatorFailureRunsInsideTransaction(t *testing.T) {
	stop := errors.New("mutator veto")
	active := false
	calls, begins := 0, 0
	d := transferMutatorFields(func(transferInput) (transferStored, error) {
		calls++
		if !active {
			t.Error("outside transaction")
		}
		return "", stop
	}, func(v transferInput) (transferStored, error) { return transferStored(v.Text), nil })
	var statements []string
	db := testDB(t, testConnector{onBegin: func() { active = true; begins++ }, statements: &statements})
	for _, run := range []func() error{
		func() error { _, e := d.Insert(t.Context(), db, d.Name.Set(transferInput{})); return e }, func() error { _, e := d.Update(t.Context(), db, 1, d.Name.Set(transferInput{})); return e }, func() error {
			_, e := d.InsertMany(t.Context(), db, [][]Assignment[transferMutated]{{d.Name.Set(transferInput{})}})
			return e
		}, func() error {
			_, e := Upsert(t.Context(), db, d.Query(), OnConflict[transferMutated]().DoNothing(), d.Name.Set(transferInput{}))
			return e
		},
	} {
		active = false
		if e := run(); !errors.Is(e, stop) {
			t.Fatal("veto identity", e)
		}
	}
	if calls != 4 || begins != 4 || len(statements) != 0 {
		t.Fatal(calls, begins, statements)
	}
	if _, e := d.InsertMany(t.Context(), db, nil); e != nil || begins != 4 {
		t.Fatal("empty batch began transaction", e)
	}
}
func TestFieldMutatorsValidateOutputAndHonorCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	notes := 0
	d := transferMutatorFields(func(v transferInput) (transferStored, error) { cancel(); return transferStored(v.Text), nil }, func(v transferInput) (transferStored, error) { notes++; return transferStored(v.Text), nil })
	p, e := planWrite(d.Query().state, querycore.InsertModel, []Assignment[transferMutated]{d.Name.Set(transferInput{}), d.Note.Set(transferInput{})}, false)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := p.mutate(ctx); !errors.Is(e, context.Canceled) || notes != 0 {
		t.Fatal("cancellation ignored", e)
	}
	stop := errors.New("invalid normalized output")
	c := codec.String[transferStored]().Validated(func(v transferStored) error {
		if v != "valid" {
			return stop
		}
		return nil
	})
	for _, out := range []transferStored{"valid", "invalid"} {
		d := transferMutatorFields(func(transferInput) (transferStored, error) { return out, nil }, func(v transferInput) (transferStored, error) { return transferStored(v.Text), nil }, Codec(c))
		p, e := planWrite(d.Query().state, querycore.InsertModel, []Assignment[transferMutated]{d.Name.Set(transferInput{"unvalidated"})}, false)
		if e != nil {
			t.Fatal(e)
		}
		p, e = p.mutate(t.Context())
		if e == nil {
			_, e = p.compile()
		}
		if out == "valid" && e != nil || out == "invalid" && !errors.Is(e, stop) {
			t.Fatal("validation used wrong phase", e)
		}
	}
}
func TestFieldMutatorsRejectInvalidDeclarationsAndInputs(t *testing.T) {
	calls := 0
	fn := func(v transferInput) (transferStored, error) { calls++; return transferStored(v.Text), nil }
	d := transferMutatorFields(fn, fn)
	name := d.Name.Set(transferInput{"private-input"})
	foreign := transferMutatorFields(fn, fn)
	wrong := name
	wrong.core.AssignmentValue = querycore.CaptureAssignment(codec.Signed[int](), 1)
	wrongNote := d.Note.Set(transferInput{})
	wrongNote.core.AssignmentValue = querycore.CaptureAssignment(codec.Signed[int](), 1)
	for _, as := range [][]Assignment[transferMutated]{{name, name}, {foreign.Name.Set(transferInput{})}, {wrong}, {name, wrongNote}} {
		p, e := planWrite(d.Query().state, querycore.InsertModel, as, false)
		if e == nil {
			_, e = p.mutate(t.Context())
		}
		if !errors.Is(e, fault.Invalid) || calls != 0 {
			t.Fatal("invalid input reached callback", e, calls)
		}
	}
	for _, bad := range []transferMutators{transferMutatorFields(nil, fn), transferMutatorFields(fn, nil)} {
		if !errors.Is(Check(bad), fault.Invalid) {
			t.Fatal("nil transform accepted")
		}
	}
}
func TestMutationInputConflictUsesFreshInputAndCopiesProposedOnce(t *testing.T) {
	calls := 0
	fn := func(v transferInput) (transferStored, error) {
		calls++
		return transferStored(strings.ToLower(strings.TrimSpace(v.Text))), nil
	}
	d := transferMutatorFields(fn, fn)
	var args [][]any
	db := testDB(t, testConnector{bindings: &args, columns: []string{"id", "name", "note"}, values: [][]driver.Value{{int64(1), "inserted", nil}}})
	for _, literal := range []bool{false, true} {
		policy := OnConflict(ConflictKey(d.Name)).DoUpdate(ConflictIncoming(d.Name))
		if literal {
			policy = policy.DoUpdate(ConflictSet(d.Name, transferInput{" UPDATED "}))
		}
		for range 2 {
			calls = 0
			if _, e := Upsert(t.Context(), db, d.Query(), policy, d.Key().Set(1), d.Name.Set(transferInput{" INSERTED "})); e != nil {
				t.Fatal(e)
			}
			want := 1
			bindings := []any{int64(1), "inserted"}
			if literal {
				want = 2
				bindings = append(bindings, "updated")
			}
			if calls != want || !reflect.DeepEqual(args[len(args)-1], bindings) {
				t.Fatal(calls, args)
			}
		}
	}
	bad := ConflictSet(d.Name, transferInput{})
	stored := querycore.CaptureAssignment(codec.String[transferStored](), transferStored("stored"))
	bad.state.update.Literal = &stored
	policy := OnConflict(ConflictKey(d.Name)).DoUpdate(bad)
	calls = 0
	if _, e := Upsert(t.Context(), db, d.Query(), policy, d.Key().Set(1), d.Name.Set(transferInput{})); !errors.Is(e, fault.Invalid) || calls != 0 {
		t.Fatal("stored conflict literal bypass", e, calls)
	}
}
func TestMutationValuesPreserveTypedInputsWithoutBinding(t *testing.T) {
	calls := 0
	c := codec.String[transferStored]().Validated(func(transferStored) error { calls++; return errors.New("must not bind") })
	fn := func(v transferInput) (transferStored, error) { t.Fatal("draft read transformed"); return "", nil }
	d := transferMutatorFields(fn, fn, Codec(c))
	as := []Assignment[transferMutated]{d.Name.Set(transferInput{"private-input"}), d.Note.SetNull()}
	p, e := planWrite(d.Query().state, querycore.InsertModel, as, false)
	if e != nil {
		t.Fatal(e)
	}
	draft := &Write[transferMutated]{state: &draftState{table: d.core, assignments: append([]querycore.MutationAssignment(nil), p.assignments...), open: true}}
	got, ok := WriteGet(draft, d.Name.Set).Get()
	if !ok || got.Text != "private-input" {
		t.Fatal("input lost")
	}
	null, ok := WriteNullableGet(draft, d.Note.Set).Get()
	if !ok || !null.IsNull() {
		t.Fatal("NULL became omitted")
	}
	if WriteGet(draft, d.Key().Set).IsSet() || !WriteIsSet(draft, d.Name.Set) {
		t.Fatal("presence changed")
	}
	foreign := transferMutatorFields(fn, fn)
	if WriteIsSet(draft, foreign.Name.Set) {
		t.Fatal("foreign presence")
	}
	draft.state.err = nil
	badSetter := func(int) Assignment[transferMutated] { return d.Name.Set(transferInput{}) }
	if WriteGet(draft, badSetter).IsSet() || !errors.Is(draft.state.err, fault.Invalid) {
		t.Fatal("wrong typed getter")
	}
	as[0] = d.Note.SetNull()
	p.assignments[0] = p.assignments[1]
	if !WriteIsSet(draft, d.Name.Set) || calls != 0 {
		t.Fatal("draft retained caller slice or bound input", calls)
	}
	for _, f := range []string{"%v", "%+v", "%#v"} {
		if strings.Contains(fmt.Sprintf(f, draft), "private-input") {
			t.Fatal("draft disclosed input")
		}
	}
}
func TestMutationValuesRejectMalformedAssignments(t *testing.T) {
	fn := func(v transferInput) (transferStored, error) { t.Fatal("invalid write transformed"); return "", nil }
	d := transferMutatorFields(fn, fn)
	a := d.Name.Set(transferInput{"secret"})
	wrong := a
	wrong.core.Field.Table = "invalid table"
	for _, as := range [][]Assignment[transferMutated]{{a, a}, {{}}, {wrong}, make([]Assignment[transferMutated], querycore.MaxExpressionNodes+1)} {
		p, e := planWrite(d.Query().state, querycore.InsertModel, as, false)
		if !errors.Is(e, fault.Invalid) || len(p.assignments) != 0 {
			t.Fatal("malformed write returned partial values", e)
		}
	}
}
