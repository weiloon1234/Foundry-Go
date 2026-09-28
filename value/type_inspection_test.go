package value_test

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/value"
)

func TestWrapperTypeInspectionDoesNotReadNilPointers(t *testing.T) {
	t.Parallel()
	if !value.IsOptionalType[value.Optional[int]]() || !value.IsOptionalType[*value.Optional[int]]() {
		t.Fatal("optional capability missing")
	}
	if value.IsOptionalType[value.Nullable[int]]() || value.IsOptionalType[int]() {
		t.Fatal("unrelated type became optional")
	}
	if !value.IsNullableType[value.Nullable[int]]() || !value.IsNullableType[*value.Nullable[int]]() {
		t.Fatal("nullable capability missing")
	}
	if value.IsNullableType[value.Optional[int]]() {
		t.Fatal("optional capability became nullable")
	}
}
