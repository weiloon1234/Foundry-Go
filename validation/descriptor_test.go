package validation_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/validation"
)

func TestDescriptorsHaveNoMethods(t *testing.T) {
	for _, descriptor := range []reflect.Type{
		reflect.TypeFor[validation.Field[Patch, Contact]](),
		reflect.TypeFor[*validation.Field[Patch, Contact]](),
		reflect.TypeFor[validation.Rule[Patch]](),
		reflect.TypeFor[*validation.Rule[Patch]](),
	} {
		if descriptor.NumMethod() != 0 {
			t.Fatalf("%v exposes %d methods", descriptor, descriptor.NumMethod())
		}
	}
}

func TestDescriptorFunctionsAsCallbacks(t *testing.T) {
	bind := validation.Rules[Patch, Contact]
	check := validation.Check[Patch]
	field := validation.DefineField("name", func(input Patch) Contact { return input.Name })
	rule := bind(field, validation.NonBlank[Contact]())
	bound := rule
	callback := func(ctx context.Context, input Patch, limits validation.Limits) error {
		return check(bound, ctx, input, limits)
	}
	rule = validation.Rule[Patch]{}
	if validation.ValidateRule(rule) == nil {
		t.Fatal("zero rule accepted")
	}
	if err := callback(t.Context(), Patch{Name: "Ada"}, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	if err := callback(t.Context(), Patch{}, validation.DefaultLimits()); err == nil {
		t.Fatal("bound rule accepted a blank name")
	}
}
