package contract

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestJSONSliceCompositionPreservesItemsAndNullability(t *testing.T) {
	descriptor := Slice(patchDescriptor())
	item := `{"id":"0193fd8c-2075-7000-8000-000000000001","age":4}`
	items, err := descriptor.Decode(context.Background(), []byte("["+item+","+item+"]"), dtoLimits())
	if err != nil || len(items) != 2 || items[0].Age != 4 || items[0].ID != items[1].ID {
		t.Fatalf("slice items changed: %v", err)
	}
	for _, input := range []string{"null", "[]"} {
		items, err := descriptor.Decode(context.Background(), []byte(input), dtoLimits())
		if err != nil || len(items) != 0 || (items == nil) != (input == "null") {
			t.Fatalf("slice representation changed for %s: %v", input, err)
		}
	}
	for _, input := range []string{"[null]", "[" + item + `,{"id":"invalid","age":1}]`, "{}"} {
		items, err := descriptor.Decode(context.Background(), []byte(input), dtoLimits())
		var failure *DecodeError
		if items != nil || !errors.As(err, &failure) {
			t.Fatalf("slice failure exposed partial items: %v", err)
		}
	}
	_, err = descriptor.Decode(context.Background(), []byte("["+item+`,{"id":"invalid","age":1}]`), dtoLimits())
	var failure *DecodeError
	if !errors.As(err, &failure) || !reflect.DeepEqual(failure.Issues(), []Issue{{Path: "/1/id", Code: ValueIssue}}) {
		t.Fatalf("array item diagnostic lost its index: %v", err)
	}
}

func TestJSONNullableCompositionCanNestWithSlices(t *testing.T) {
	descriptor := Nullable(patchDescriptor())
	item := `{"id":"0193fd8c-2075-7000-8000-000000000001","age":7}`
	null, err := descriptor.Decode(context.Background(), []byte("null"), dtoLimits())
	if err != nil || !null.IsNull() {
		t.Fatalf("explicit null changed: %v", err)
	}
	present, err := descriptor.Decode(context.Background(), []byte(item), dtoLimits())
	record, set := present.Get()
	if err != nil || !set || record.Age != 7 {
		t.Fatalf("nullable DTO value changed: %v", err)
	}
	invalid, err := descriptor.Decode(context.Background(), []byte(`{"age":7}`), dtoLimits())
	if err == nil || !invalid.IsNull() {
		t.Fatal("nullable failure exposed a partial record")
	}
	items, err := Slice(descriptor).Decode(context.Background(), []byte("[null,"+item+"]"), dtoLimits())
	if err != nil || len(items) != 2 || !items[0].IsNull() || items[1].IsNull() {
		t.Fatalf("nullable slice items changed: %v", err)
	}
	list, err := Nullable(Slice(patchDescriptor())).Decode(context.Background(), []byte("["+item+"]"), dtoLimits())
	values, set := list.Get()
	if err != nil || !set || len(values) != 1 || values[0].Age != 7 {
		t.Fatalf("nullable slice changed: %v", err)
	}
}

func TestJSONCompositionOwnsSchemaAndRetainsBounds(t *testing.T) {
	base := patchDescriptor()
	before, err := base.Description()
	if err != nil {
		t.Fatal(err)
	}
	descriptor := Slice(Nullable(base))
	one, err := descriptor.Description()
	if err != nil {
		t.Fatal(err)
	}
	two, err := Slice(Nullable(base)).Description()
	if err != nil || !reflect.DeepEqual(one, two) {
		t.Fatalf("composition is not deterministic: %v", err)
	}
	one.Types[0].ID = "caller edit"
	after, err := base.Description()
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("composition changed its element descriptor")
	}
	current, err := descriptor.Description()
	if err != nil || !reflect.DeepEqual(two, current) {
		t.Fatal("caller changed the composed descriptor")
	}
	limits := dtoLimits()
	limits.Steps = 1
	items, err := descriptor.Decode(context.Background(), []byte("[null]"), limits)
	if err == nil || items != nil {
		t.Fatal("composition bypassed the work bound")
	}
	for _, err := range []error{Slice(JSON[PatchDTO]{}).Validate(), Nullable(JSON[PatchDTO]{}).Validate()} {
		if !errors.Is(err, fault.Invalid) {
			t.Fatal("composition accepted an invalid element")
		}
	}
}
