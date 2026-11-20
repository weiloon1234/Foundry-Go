package validation

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
)

type sampleBoundRow struct {
	ID   int64
	Name string
}

func TestBindingValidationAndAccess(t *testing.T) {
	b := DefineBinding("name", func(r *sampleBoundRow) *string { return &r.Name })
	if err := ValidateBinding(b); err != nil {
		t.Fatalf("ValidateBinding: %v", err)
	}
	if got := BindingName(b); got != "name" {
		t.Fatalf("BindingName = %q, want name", got)
	}
	row := sampleBoundRow{ID: 7, Name: "Ada"}
	ptr := BoundPointer(b, &row)
	if ptr != &row.Name {
		t.Fatal("BoundPointer did not return exact struct field address")
	}
	*ptr = "Grace"
	if got := *BoundPointer(b, &row); got != "Grace" {
		t.Fatalf("*BoundPointer = %q, want Grace", got)
	}
	if BoundPointer(b, nil) != nil {
		t.Fatal("BoundPointer(b, nil) must return nil")
	}
}

func TestBindingRejectsInvalidDeclarations(t *testing.T) {
	cases := []struct {
		name string
		b    Binding[sampleBoundRow, string]
	}{
		{"zero", Binding[sampleBoundRow, string]{}},
		{"empty_name", DefineBinding("", func(r *sampleBoundRow) *string { return &r.Name })},
		{"nil_pointer", DefineBinding[sampleBoundRow, string]("name", nil)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateBinding(tc.b); !errors.Is(err, fault.Invalid) {
				t.Fatalf("ValidateBinding(%s) = %v, want fault.Invalid", tc.name, err)
			}
			if BoundPointer(tc.b, &sampleBoundRow{}) != nil {
				t.Fatalf("BoundPointer(%s) expected nil", tc.name)
			}
		})
	}
	nilResult := DefineBinding("name", func(*sampleBoundRow) *string { return nil })
	if BoundPointer(nilResult, &sampleBoundRow{}) != nil {
		t.Fatal("BoundPointer(nil result) expected nil")
	}
}
