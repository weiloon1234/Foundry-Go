package value_test

import (
	"encoding/json"
	"testing"

	"github.com/weiloon1234/Foundry-Go/value"
)

type patch struct {
	Name  value.Optional[value.Nullable[string]] `json:"name,omitzero"`
	Count value.Optional[int]                    `json:"count,omitzero"`
}

func TestNullableTypeCapability(t *testing.T) {
	type alias = value.Nullable[string]
	type ordinary struct{ Present bool }
	if !value.IsNullableType[value.Nullable[int]]() || !value.IsNullableType[alias]() || value.IsNullableType[int]() || value.IsNullableType[value.Optional[int]]() || value.IsNullableType[ordinary]() {
		t.Fatal("nullable type capability does not match the declared value boundary")
	}
}

func TestOmittedNullAndPresentZeroStayDistinct(t *testing.T) {
	for _, input := range []string{`{}`, `{"name":null}`, `{"name":""}`, `{"count":0}`, `{"name":"Ada","count":3}`} {
		var p patch
		if err := json.Unmarshal([]byte(input), &p); err != nil {
			t.Fatal(err)
		}
		out, err := json.Marshal(p)
		if err != nil || string(out) != input {
			t.Fatalf("round trip %s -> %s: %v", input, out, err)
		}
		if input == `{"name":null}` {
			nullable, set := p.Name.Get()
			if !set || !nullable.IsNull() {
				t.Fatal("explicit null became missing")
			}
		}
		if input == `{"count":0}` {
			n, set := p.Count.Get()
			if !set || n != 0 {
				t.Fatal("zero became missing")
			}
		}
	}
}

func TestInvalidDecodingLeavesValueUnchanged(t *testing.T) {
	v := value.Set(3)
	for _, input := range []string{`null`, `"three"`, `[]`} {
		if err := json.Unmarshal([]byte(input), &v); err == nil {
			t.Fatal("invalid input accepted")
		}
		if n, set := v.Get(); !set || n != 3 {
			t.Fatal("failure mutated target")
		}
	}
	if _, err := json.Marshal(value.Optional[int]{}); err == nil {
		t.Fatal("omission encoded as a value")
	}
	if _, err := json.Marshal(value.Of[*int](nil)); err == nil {
		t.Fatal("present nil silently became null")
	}
	null := value.Null[string]()
	if _, valid := null.Get(); valid {
		t.Fatal("null became valid")
	}
}
