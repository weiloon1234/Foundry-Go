package contractmeta

import (
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
)

type envelope struct {
	Value    string     `json:"value"`
	Children []envelope `json:"children,omitempty"`
}
type unlisted struct {
	Hidden string `json:"hidden"`
}
type outer struct{ unlisted }
type bytesEnvelope struct {
	Bytes []byte `json:"bytes"`
}
type customText string

func (customText) MarshalJSON() ([]byte, error) { return []byte(`42`), nil }

type customEnvelope struct {
	Text customText `json:"text"`
}

func TestClosedFrameworkStructuresPreserveRecursionAndOptionality(t *testing.T) {
	typ := reflect.TypeFor[envelope]()
	schema, err := Struct(typ, []reflect.Type{typ}, nil)
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := schema.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	var root contract.Type
	for _, node := range normalized.Types {
		if node.ID == normalized.Root {
			root = node
		}
	}
	if len(root.Properties) != 2 || root.Properties[0].Name != "children" || root.Properties[0].Required || !root.Properties[1].Required {
		t.Fatal("wire field presence was lost")
	}
	for _, typ := range []reflect.Type{reflect.TypeFor[outer](), reflect.TypeFor[bytesEnvelope](), reflect.TypeFor[customEnvelope]()} {
		if _, err := Struct(typ, []reflect.Type{typ}, nil); err == nil {
			t.Fatal("unlisted or custom wire shape inferred")
		}
	}
}
