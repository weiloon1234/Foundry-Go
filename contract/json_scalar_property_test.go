package contract

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/value"
)

type ScalarColumnDTO struct {
	State    string                                 `json:"state"`
	Count    *int64                                 `json:"count,string"`
	Label    value.Optional[value.Nullable[string]] `json:"label,omitzero"`
	Children []string                               `json:"children"`
	Dynamic  any                                    `json:"dynamic"`
}

func scalarColumnDescriptor() JSON[ScalarColumnDTO] {
	root := dtoRoot[ScalarColumnDTO]()
	return DefineJSON[ScalarColumnDTO](Schema{Root: root, Types: []Type{
		{ID: root, Kind: ObjectKind, Properties: []Property{
			{Name: "state", Type: "state", Required: true}, {Name: "count", Type: "quoted", Required: true},
			{Name: "label", Type: "optional"}, {Name: "children", Type: "children", Required: true}, {Name: "dynamic", Type: "dynamic", Required: true},
		}},
		{ID: "state", Kind: StringKind, Cases: []json.RawMessage{json.RawMessage(`"ready"`), json.RawMessage(`"draft"`)}},
		{ID: "quoted", Kind: QuotedKind, Element: "integer", Nullable: true},
		{ID: "integer", Kind: IntegerKind, Bits: 64, Signed: true},
		{ID: "optional", Kind: AliasKind, Element: "nullable", Nullable: true},
		{ID: "nullable", Kind: AliasKind, Element: "string", Nullable: true},
		{ID: "string", Kind: StringKind},
		{ID: "children", Kind: ArrayKind, Element: "string", Nullable: true},
		{ID: "dynamic", Kind: DynamicKind, Nullable: true},
	}})
}

func TestJSONScalarPropertyResolvesWireWrappersAndOwnsMetadata(t *testing.T) {
	descriptor := scalarColumnDescriptor()
	if err := descriptor.Validate(); err != nil {
		t.Fatal(err)
	}
	count, err := descriptor.DescribeScalarProperty("count")
	if err != nil || !count.Quoted || !count.Value.Nullable || count.Value.Kind != IntegerKind || count.Value.Bits != 64 || !count.Value.Signed || !count.Property.Required {
		t.Fatal("quoted nullable integer lost its typed semantics", count, err)
	}
	label, err := descriptor.DescribeScalarProperty("label")
	if err != nil || label.Quoted || !label.Value.Nullable || label.Value.Kind != StringKind || label.Property.Required {
		t.Fatal("optional nullable label changed", label, err)
	}
	state, err := descriptor.DescribeScalarProperty("state")
	if err != nil || state.Value.Nullable || len(state.Value.Cases) != 2 {
		t.Fatal(state, err)
	}
	before, _ := descriptor.Description()
	state.Value.Cases[0][1] = 'x'
	state.Property.Name = "changed"
	count.Value.Nullable = false
	after, _ := descriptor.Description()
	if !reflect.DeepEqual(before, after) {
		t.Fatal("property metadata mutated the owned graph")
	}
	for _, name := range []string{"missing", "State", "children", "dynamic", ""} {
		if info, err := descriptor.DescribeScalarProperty(name); err == nil || !reflect.DeepEqual(info, ScalarProperty{}) {
			t.Fatal("non-scalar property exposed metadata", name, err)
		}
	}
	var zero JSON[ScalarColumnDTO]
	if info, err := zero.DescribeScalarProperty("state"); err == nil || !reflect.DeepEqual(info, ScalarProperty{}) {
		t.Fatal("zero descriptor accepted")
	}
}

func TestMetadataScalarSelectionPreservesRequestedOrder(t *testing.T) {
	schema, err := scalarColumnDescriptor().Description()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	var restored Schema
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	selected, err := restored.ScalarProperties("state", "count")
	if err != nil || len(selected) != 2 || selected[0].Property.Name != "state" || !selected[1].Quoted {
		t.Fatal("selected scalar inspection changed", err)
	}
	for _, names := range [][]string{{"state", "state"}, {"children"}, {"missing"}} {
		if _, err := restored.ScalarProperties(names...); err == nil {
			t.Fatal("invalid selection accepted")
		}
	}
}
