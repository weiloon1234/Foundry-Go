package codec_test

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestJSONCodecSeparatesNullAndOwnsScan(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
	}
	c := codec.JSON[value.Nullable[payload]]()
	doc, err := value.NewJSON(value.Of(payload{Name: "typed"}))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := c.Bind(doc)
	if err != nil || c.ParameterType() != codec.TypeJSON {
		t.Fatal(encoded, err)
	}
	if got, err := c.Decode([]byte(encoded.(string))); err != nil || got != doc {
		t.Fatal(got, err)
	}
	current := doc
	for _, bad := range []any{nil, 42, `{"name":null}`, `{}`, `{"name":"ok","unknown":true}`} {
		if err := c.Scan(&current).Scan(bad); err == nil || current != doc {
			t.Fatal("invalid scan changed destination", err)
		}
	}
	if _, err := c.Bind(value.JSON[value.Nullable[payload]]{}); err == nil {
		t.Fatal("zero bound")
	}
	nullable := codec.Nullable(c)
	if got, err := nullable.Decode(nil); err != nil || !got.IsNull() {
		t.Fatal(got, err)
	}
	jsonNull, err := c.Decode("null")
	if err != nil || !jsonNull.IsJSONNull() {
		t.Fatal(jsonNull, err)
	}
	if bound, err := nullable.Bind(value.Of(jsonNull)); err != nil || bound != "null" {
		t.Fatal(bound, err)
	}
	if got, err := nullable.Decode("null"); err != nil || got.IsNull() {
		t.Fatal("JSON null became SQL NULL", err)
	}
}
