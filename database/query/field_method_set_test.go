package query

import (
	"errors"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// A method-bearing embedded base generates another set of concrete/pointer
// wrappers for every field owner/value pair. Keep storage as typed data only;
// the public scalar/binary capabilities own their methods and share functions.
func TestSharedFieldStorageHasNoCapabilityMethodSet(t *testing.T) {
	for _, typ := range []reflect.Type{
		reflect.TypeFor[valueField[user, string]](),
		reflect.TypeFor[valueField[user, []byte]](),
		reflect.TypeFor[valueField[user, value.Nullable[string]]](),
	} {
		if typ.NumMethod() != 0 || reflect.PointerTo(typ).NumMethod() != 0 {
			t.Errorf("shared storage %v must not own a capability method set", typ)
		}
	}
}

// Optional capabilities must not re-enter every generated field's method set.
// Reflection (including text/template) retains these wrappers in consumers.
func TestFieldComputationsAreOptIn(t *testing.T) {
	for _, typ := range []reflect.Type{
		reflect.TypeFor[ScalarField[user, string]](),
		reflect.TypeFor[NullableTextField[user, string]](),
		reflect.TypeFor[BinaryField[user, []byte]](),
		reflect.TypeFor[NullableBinaryField[user, []byte]](),
		reflect.TypeFor[ExactField[user, int64]](),
		reflect.TypeFor[NullableFloatField[user, float32]](),
	} {
		for _, name := range []string{"Value", "Param", "Count", "CountDistinct", "Min", "Max", "Sum", "Avg"} {
			if _, ok := typ.MethodByName(name); ok {
				t.Errorf("%v unexpectedly retains optional %s method", typ, name)
			}
		}
	}
}

func TestFieldCapabilityFunctionsPreservePointersNullsAndInvalidDescriptors(t *testing.T) {
	field := NewNullableExactField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	// These assignments prove nullable selection and non-null binding stay separate.
	var selected Expression[cursorRecord, value.Nullable[int64]] = Value(&field)
	var parameter RowExpression[cursorRecord, int64] = Param(&field, int64(3))
	if _, err := SelectValue(cursorQuery(), selected).Compile(); err != nil {
		t.Fatal(err)
	}
	if _, err := SelectValue(cursorQuery(), parameter.Value()).Compile(); err != nil {
		t.Fatal(err)
	}
	var missing *NullableExactField[cursorRecord, int64]
	checks := []func() error{
		func() error { _, e := SelectValue(cursorQuery(), Value(missing)).Compile(); return e },
		func() error { _, e := SelectValue(cursorQuery(), Param(missing, int64(3)).Value()).Compile(); return e },
		func() error { _, e := SelectValue(cursorQuery(), CountField(missing).Value()).Compile(); return e },
		func() error {
			_, e := SelectValue(cursorQuery(), CountDistinctField(missing).Value()).Compile()
			return e
		},
		func() error { _, e := SelectValue(cursorQuery(), Min(missing).Value()).Compile(); return e },
		func() error { _, e := SelectValue(cursorQuery(), Max(missing).Value()).Compile(); return e },
		func() error { _, e := SelectValue(cursorQuery(), Sum(missing).Value()).Compile(); return e },
		func() error { _, e := SelectValue(cursorQuery(), Avg(missing).Value()).Compile(); return e },
	}
	for i, check := range checks {
		if e := check(); !errors.Is(e, fault.Invalid) {
			t.Errorf("nil descriptor check %d = %v", i, e)
		}
	}
}
