package datatable

import (
	"encoding/json"
	"testing"
)

func TestSerializedTableMetadataKeepsRowFiltersAndOrderingConsistent(t *testing.T) {
	info, err := Define(reportSpec()).Description()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	var restored Description
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	normalized, err := restored.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	if normalized.Columns[0].Name != info.Columns[0].Name || !normalized.Columns[0].Value.Quoted || !normalized.Columns[2].Value.Value.Nullable {
		t.Fatal("row scalar metadata lost wire state")
	}
	for _, change := range []func(*Description){
		func(d *Description) { d.Columns[0].Value.Property.Required = !d.Columns[0].Value.Property.Required },
		func(d *Description) { d.Columns[0].Value.Value.ID = "other" },
		func(d *Description) { d.DefaultSort[0].Column = "missing" },
		func(d *Description) { d.Columns[1].Filter.Operators = []Operator{All} },
	} {
		var copy Description
		if err := json.Unmarshal(data, &copy); err != nil {
			t.Fatal(err)
		}
		change(&copy)
		if _, err := copy.Normalize(); err == nil {
			t.Fatal("metadata disagrees with typed declaration")
		}
	}
	normalized.Columns[1].Filter.Operators[0] = "changed"
	if restored.Columns[1].Filter.Operators[0] == "changed" {
		t.Fatal("normalized metadata aliases source")
	}
}
