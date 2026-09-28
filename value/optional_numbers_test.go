package value_test

import (
	"encoding/json"
	"testing"

	"github.com/weiloon1234/Foundry-Go/value"
)

func TestWrappersPreserveDynamicJSONNumbers(t *testing.T) {
	const literal = "9007199254740993"
	var optional value.Optional[any]
	if err := json.Unmarshal([]byte(literal), &optional); err != nil {
		t.Fatal(err)
	}
	if got, set := optional.Get(); !set || got != json.Number(literal) {
		t.Fatalf("optional number changed: %T %v", got, got)
	}
	var nullable value.Nullable[any]
	if err := json.Unmarshal([]byte(literal), &nullable); err != nil {
		t.Fatal(err)
	}
	if got, set := nullable.Get(); !set || got != json.Number(literal) {
		t.Fatalf("nullable number changed: %T %v", got, got)
	}
	type Record struct {
		Data value.Optional[value.Nullable[map[string]any]] `json:"data,omitzero"`
	}
	record, err := value.ParseJSON[Record](`{"data":{"exact":9007199254740993}}`)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := record.Decode()
	if err != nil {
		t.Fatal(err)
	}
	present, _ := decoded.Data.Get()
	data, _ := present.Get()
	if got := data["exact"]; got != json.Number(literal) {
		t.Fatalf("nested snapshot number changed: %T %v", got, got)
	}
}

func TestWrapperDirectDecodingRejectsTrailingValuesWithoutMutation(t *testing.T) {
	optional := value.Set(7)
	nullable := value.Of(7)
	for _, input := range []string{"1 2", "1 trailing", "1 null"} {
		if err := optional.UnmarshalJSON([]byte(input)); err == nil {
			t.Fatal("optional accepted trailing input")
		}
		if got, set := optional.Get(); !set || got != 7 {
			t.Fatal("optional mutated after invalid input")
		}
		if err := nullable.UnmarshalJSON([]byte(input)); err == nil {
			t.Fatal("nullable accepted trailing input")
		}
		if got, set := nullable.Get(); !set || got != 7 {
			t.Fatal("nullable mutated after invalid input")
		}
	}
}
