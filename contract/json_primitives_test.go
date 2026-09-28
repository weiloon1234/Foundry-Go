package contract

import (
	"encoding/json"
	"math"
	"testing"
)

func TestPrimitiveJSONRetainsExactValuesAndNativeWidths(t *testing.T) {
	limits := JSONLimits{Bytes: 1024, Depth: 8, Nodes: 100, Steps: 1000, Issues: 8}
	if raw, err := StringJSON[string]().Encode(t.Context(), "hello", limits); err != nil || string(raw) != `"hello"` {
		t.Fatal("string descriptor", err)
	}
	if raw, err := BooleanJSON[bool]().Encode(t.Context(), true, limits); err != nil || string(raw) != "true" {
		t.Fatal("boolean descriptor", err)
	}
	if n, err := IntegerJSON[uint64]().Decode(t.Context(), []byte("18446744073709551615"), limits); err != nil || n != math.MaxUint64 {
		t.Fatal("integer precision", err)
	}
	for _, raw := range []string{"-1", "256", "1.1", "null"} {
		if _, err := IntegerJSON[uint8]().Decode(t.Context(), []byte(raw), limits); err == nil {
			t.Fatal("invalid uint8 value accepted", raw)
		}
	}
	if _, err := NumberJSON[float64]().Encode(t.Context(), math.Inf(1), limits); err == nil {
		t.Fatal("non-finite scalar accepted")
	}
	if raw, err := DynamicJSON().Decode(t.Context(), []byte(`{"count":9007199254740993}`), limits); err != nil || !json.Valid(raw) {
		t.Fatal("dynamic facility", err)
	}
	if _, err := DynamicJSON().Decode(t.Context(), []byte(`{"same":1,"same":2}`), limits); err == nil {
		t.Fatal("dynamic duplicate names accepted")
	}
}
