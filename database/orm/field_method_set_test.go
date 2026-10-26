package orm

import (
	"github.com/weiloon1234/Foundry-Go/value"
	"reflect"
	"testing"
)

func TestSharedFieldStorageHasNoCapabilityMethodSet(t *testing.T) {
	for _, typ := range []reflect.Type{reflect.TypeFor[columnData[string]](), reflect.TypeFor[columnData[[]byte]](), reflect.TypeFor[columnData[value.Nullable[string]]]()} {
		if typ.NumMethod() != 0 || reflect.PointerTo(typ).NumMethod() != 0 {
			t.Fatalf("shared storage %v owns capability methods", typ)
		}
	}
}
func TestFieldComputationsAreOptIn(t *testing.T) {
	for _, typ := range []reflect.Type{reflect.TypeFor[Column[record, string]](), reflect.TypeFor[NullableText[record, string]](), reflect.TypeFor[Column[record, []byte]](), reflect.TypeFor[NullableColumn[record, []byte]](), reflect.TypeFor[Number[record, int64]](), reflect.TypeFor[NullableNumber[record, float32]]()} {
		for _, name := range []string{"Value", "Param", "Count", "CountDistinct", "Min", "Max", "Sum", "Avg"} {
			if _, ok := typ.MethodByName(name); ok {
				t.Errorf("%v retains optional %s method", typ, name)
			}
		}
	}
}
