package contract

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
)

func TestJSONEncodeChecksResponseSchemaBeforeReturningBytes(t *testing.T) {
	id, err := model.ParseID[ProfileModel]("0193fd8c-2075-7000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	input := PatchDTO{ID: id, Age: 4}
	data, err := patchDescriptor().Encode(context.Background(), input, dtoLimits())
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := patchDescriptor().Decode(context.Background(), data, dtoLimits())
	if err != nil || decoded.ID != id || decoded.Age != 4 {
		t.Fatalf("encoded response lost concrete values: %v", err)
	}
	// model.ID's serialization contract explicitly preserves the nil UUID.
	input.ID = model.ID[ProfileModel]{}
	data, err = patchDescriptor().Encode(context.Background(), input, dtoLimits())
	if err != nil {
		t.Fatalf("nil UUID serialization changed: %v", err)
	}
	decoded, err = patchDescriptor().Decode(context.Background(), data, dtoLimits())
	if err != nil || !decoded.ID.IsZero() {
		t.Fatal("nil UUID was not retained")
	}
	input.Data = math.Inf(1)
	data, err = patchDescriptor().Encode(context.Background(), input, dtoLimits())
	var failure *EncodeError
	if data != nil || !errors.As(err, &failure) || !errors.Is(err, fault.Internal) {
		t.Fatalf("invalid response exposed bytes: %v", err)
	}
}

type ResponseShapeDTO struct {
	Value string `json:"value"`
}

func TestJSONEncodeRejectsSchemaMismatchAndOwnsIssues(t *testing.T) {
	root := dtoRoot[ResponseShapeDTO]()
	descriptor := DefineJSON[ResponseShapeDTO](Schema{Root: root, Types: []Type{
		{ID: root, Kind: ObjectKind, Properties: []Property{{Name: "value", Type: "integer", Required: true}}},
		{ID: "integer", Kind: IntegerKind, Bits: 32, Signed: true},
	}})
	data, err := descriptor.Encode(context.Background(), ResponseShapeDTO{"private value"}, dtoLimits())
	var failure *EncodeError
	if data != nil || !errors.As(err, &failure) || strings.Contains(err.Error(), "private") {
		t.Fatalf("mismatched output escaped: %v", err)
	}
	issues := failure.Issues()
	if len(issues) != 1 || issues[0].Path != "/value" || issues[0].Code != TypeIssue {
		t.Fatal("response field diagnostic changed")
	}
	issues[0].Path = "changed"
	if failure.Issues()[0].Path == "changed" {
		t.Fatal("caller changed response diagnostics")
	}
}
