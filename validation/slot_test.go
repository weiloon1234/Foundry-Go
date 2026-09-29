package validation_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/validation"
)

type renameRequest struct {
	ID   int
	Name string
}

// ownedNames is a typed lookup whose scope excludes the row supplied through a
// slot, the shape of "unique except the record being updated".
type ownedNames struct {
	current validation.Slot[int]
	rows    map[string]int
	calls   atomic.Int32
}

func (*ownedNames) Validate() error { return nil }
func (l *ownedNames) Exists(ctx context.Context, name string) (bool, error) {
	l.calls.Add(1)
	ignored, err := l.current.Value(ctx)
	if err != nil {
		return false, err
	}
	owner, found := l.rows[name]
	return found && owner != ignored, nil
}

func TestSlotsParameterizeOneDeclarationPerCheck(t *testing.T) {
	t.Parallel()
	current := validation.NewSlot[int]()
	lookup := &ownedNames{current: current, rows: map[string]int{"alpha": 1, "beta": 2}}
	name := validation.DefineField("name", func(input renameRequest) string { return input.Name })
	unique := validation.Requires(current, validation.Unique[string](lookup))
	if validation.All(name.Rules(unique)).Validate() == nil {
		t.Fatal("slot reader accepted without a provider")
	}
	if _, err := name.Rules(unique).Description(); err != nil {
		t.Fatal("partial tree could not be described", err)
	}
	rule := validation.Provide(current, func(_ context.Context, input renameRequest) (int, error) { return input.ID, nil }, name.Rules(unique))
	if err := rule.Validate(); err != nil {
		t.Fatal(err)
	}
	// The same declaration keeps the current row's own name and rejects another row's.
	if err := rule.Check(t.Context(), renameRequest{ID: 1, Name: "alpha"}, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	issues := rejection(t, rule.Check(t.Context(), renameRequest{ID: 2, Name: "alpha"}, validation.DefaultLimits())).Issues()
	if len(issues) != 1 || issues[0].Path != "/name" || issues[0].Code != "foundry.unique" {
		t.Fatalf("slot-scoped rejection: %+v", issues)
	}
	// Parallel branches and conditions observe the provided value.
	parallel := validation.Provide(current, func(_ context.Context, input renameRequest) (int, error) { return input.ID, nil },
		validation.Parallel(name.Rules(unique), validation.When(name.Rules(unique), name.Rules(unique))))
	if err := parallel.Check(t.Context(), renameRequest{ID: 2, Name: "beta"}, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	// Reading the slot outside Provide fails execution instead of widening the scope.
	unprovided := validation.Unique[string](lookup)
	if err := unprovided.Check(t.Context(), "alpha", validation.DefaultLimits()); !errors.Is(err, fault.Internal) {
		t.Fatal("missing slot did not fail execution", err)
	}
	if _, err := current.Value(t.Context()); err == nil {
		t.Fatal("slot value exposed outside a check")
	}
}

func TestSlotProviderFailuresAndDeclarations(t *testing.T) {
	t.Parallel()
	current := validation.NewSlot[int]()
	reader := validation.Requires(current, validation.Custom(validation.Spec{ID: "app.slot", Message: "Invalid."}, func(ctx context.Context, _ string) (bool, error) {
		value, err := current.Value(ctx)
		return value == 7, err
	}))
	private := errors.New("private provider failure")
	failing := validation.Provide(current, func(context.Context, string) (int, error) { return 0, private }, reader)
	err := failing.Check(t.Context(), "input", validation.DefaultLimits())
	if !errors.Is(err, fault.Internal) {
		t.Fatal("provider failure was not an execution failure", err)
	}
	var rejected *validation.Errors
	if errors.As(err, &rejected) {
		t.Fatal("provider failure became a rejection")
	}
	nested := validation.Provide(current, func(context.Context, string) (int, error) { return 1, nil },
		validation.Provide(current, func(context.Context, string) (int, error) { return 7, nil }, reader))
	if err := nested.Check(t.Context(), "input", validation.DefaultLimits()); err != nil {
		t.Fatal("inner provider did not win", err)
	}
	var zero validation.Slot[int]
	for _, bad := range []validation.Rule[string]{
		validation.Provide(zero, func(context.Context, string) (int, error) { return 0, nil }, reader),
		validation.Provide[string](current, nil, reader),
		validation.Requires(zero, validation.NonBlank[string]()),
	} {
		if bad.Validate() == nil {
			t.Fatal("invalid slot declaration accepted")
		}
	}
	other := validation.NewSlot[int]()
	wrong := validation.Provide(other, func(context.Context, string) (int, error) { return 7, nil }, reader)
	if wrong.Validate() == nil {
		t.Fatal("a different slot satisfied the requirement")
	}
}
