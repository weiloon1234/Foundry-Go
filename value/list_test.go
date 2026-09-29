package value_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/weiloon1234/Foundry-Go/value"
)

func TestListIsNeverNull(t *testing.T) {
	type payload struct {
		Items value.List[string] `json:"items"`
	}
	limits := value.JSONEncodingLimits{Bytes: 1024, Depth: 8, Nodes: 64, Steps: 256}
	for _, input := range []payload{{}, {Items: value.List[string]{}}} {
		data, err := value.EncodeJSON(context.Background(), input, limits)
		if err != nil || string(data) != `{"items":[]}` {
			t.Fatalf("encoded %s, %v", data, err)
		}
		if legacy, err := json.Marshal(input); err != nil || string(legacy) != `{"items":[]}` {
			t.Fatalf("encoding/json encoded %s, %v", legacy, err)
		}
	}
	data, err := value.EncodeJSON(context.Background(), payload{Items: value.List[string]{"a", "b"}}, limits)
	if err != nil || string(data) != `{"items":["a","b"]}` {
		t.Fatalf("encoded %s, %v", data, err)
	}
	if _, err := value.EncodeJSON(context.Background(), payload{Items: value.List[string]{"bad\xff"}}, limits); err == nil {
		t.Fatal("invalid UTF-8 inside a list was accepted")
	}
	var decoded payload
	if err := json.Unmarshal([]byte(`{"items":null}`), &decoded); err == nil {
		t.Fatal("null list decoded")
	}
	if err := json.Unmarshal([]byte(`{"items":[]}`), &decoded); err != nil || decoded.Items == nil || len(decoded.Items) != 0 {
		t.Fatalf("empty list = %#v, %v", decoded.Items, err)
	}
}
