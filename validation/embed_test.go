package validation_test

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/validation"
)

func TestEmbeddedValidationKeepsWirePathsAndFailures(t *testing.T) {
	type outer struct{ Patch Patch }
	inner := patchRules()
	embedded := validation.Embed(inner, func(input outer) Patch { return input.Patch })
	in := Patch{Name: " "}
	one := rejection(t, inner.Check(t.Context(), in, validation.DefaultLimits())).Issues()
	two := rejection(t, embedded.Check(t.Context(), outer{Patch: in}, validation.DefaultLimits())).Issues()
	if !reflect.DeepEqual(one, two) {
		t.Fatal("embedding changed paths or rules")
	}
	innerInfo, _ := inner.Description()
	info, err := embedded.Description()
	if err != nil || len(info.Children) != 1 || !reflect.DeepEqual(info.Children[0], innerInfo) {
		t.Fatal("embedded rule metadata changed")
	}
	if validation.Embed[outer](inner, nil).Validate() == nil {
		t.Fatal("nil selector accepted")
	}
	rule := validation.Embed(inner, func(outer) Patch { panic("private") })
	if err := rule.Check(t.Context(), outer{}, validation.DefaultLimits()); err == nil || errors.Is(err, fault.Invalid) {
		t.Fatalf("selector failure classified as user input: %v", err)
	}
	// With an application callback in the tree the check owns its goroutine, so
	// a selector Goexit is also an internal failure.
	exiting := validation.All(
		validation.Custom(validation.Spec{ID: "app.embedded", Message: "Embedded."}, func(context.Context, outer) (bool, error) { return true, nil }),
		validation.Embed(inner, func(outer) Patch { runtime.Goexit(); return Patch{} }),
	)
	if err := exiting.Check(t.Context(), outer{}, validation.DefaultLimits()); err == nil || errors.Is(err, fault.Invalid) {
		t.Fatalf("selector exit classified as user input: %v", err)
	}
}
