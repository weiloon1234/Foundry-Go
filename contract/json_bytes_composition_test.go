package contract

import (
	"bytes"
	"encoding/json"
	"strconv"
	"testing"
)

type EncodedByte uint8

func (v EncodedByte) MarshalJSON() ([]byte, error) {
	return strconv.AppendUint(nil, uint64(v), 10), nil
}
func (v *EncodedByte) UnmarshalJSON(data []byte) error {
	var n uint8
	if err := json.Unmarshal(data, &n); err != nil {
		return err
	}
	*v = EncodedByte(n)
	return nil
}

func TestJSONByteSliceUsesNativeRepresentation(t *testing.T) {
	descriptor := Slice(IntegerJSON[byte]())
	encoded, err := descriptor.Encode(t.Context(), []byte("ab"), dtoLimits())
	if err != nil || string(encoded) != `"YWI="` {
		t.Fatalf("native bytes changed: %s %v", encoded, err)
	}
	decoded, err := descriptor.Decode(t.Context(), encoded, dtoLimits())
	if err != nil || !bytes.Equal(decoded, []byte("ab")) {
		t.Fatal("byte roundtrip", err)
	}
	for _, wire := range []string{`""`, `null`} {
		decoded, err := descriptor.Decode(t.Context(), []byte(wire), dtoLimits())
		if err != nil || len(decoded) != 0 || (decoded == nil) != (wire == "null") {
			t.Fatal("empty/null bytes changed", err)
		}
	}
	if _, err := descriptor.Decode(t.Context(), []byte(`[97,98]`), dtoLimits()); err == nil {
		t.Fatal("array bypassed declared base64 wire shape")
	}
	root := dtoRoot[EncodedByte]()
	custom := DefineJSONValue[EncodedByte](Schema{Root: root, Types: []Type{{ID: root, Kind: IntegerKind, Bits: 8}}})
	encoded, err = Slice(custom).Encode(t.Context(), []EncodedByte{1, 2}, dtoLimits())
	if err != nil || string(encoded) != "[1,2]" {
		t.Fatal("custom byte codec was erased", string(encoded), err)
	}
}

func TestJSONByteSliceCannotDiscardElementRules(t *testing.T) {
	for _, wire := range []Type{
		{ID: "uint8", Kind: IntegerKind, Bits: 8, Cases: []json.RawMessage{json.RawMessage("1")}},
		{ID: "uint8", Kind: IntegerKind, Bits: 16},
		{ID: "uint8", Kind: IntegerKind, Bits: 8, Nullable: true},
		{ID: "uint8", Kind: StringKind},
	} {
		element := DefineJSONField[uint8](Schema{Root: wire.ID, Types: []Type{wire}})
		if Slice(element).Validate() == nil {
			t.Fatal("byte element contract silently widened")
		}
	}
}
