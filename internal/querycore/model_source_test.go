package querycore

import (
	"reflect"
	"testing"
)

func TestRelationSourcesRetainModelSensitivity(t *testing.T) {
	model := ModelDefinition{Table: "items", Columns: []Column{{Name: "id"}, {Name: "secret"}}, Sensitive: map[string]bool{"secret": true}}
	q := QueryCore{Table: "items", Model: &model}
	through := RelationThroughSelect(q, q, FieldRef{Column: "id"}, FieldRef{Column: "id"}, nil, "target", "pivot")
	hop := RelationHopJoin(q.ModelSelect(nil), "items", FieldRef{Column: "id"}, model.Source("items", ""), nil, FieldRef{Column: "id"})
	for name, node := range map[string]SelectNode{"ordinary": q.ModelSelect(ColumnSelections("items", model.Columns)), "through": through, "hop": hop} {
		t.Run(name, func(t *testing.T) {
			table := node.Source.Name()
			node.Selections = []SelectItem{{Expression: FieldRef{Table: table, Column: "id"}}, {Expression: FieldRef{Table: table, Column: "secret"}}}
			if len(node.Joins) > 0 {
				node.Selections = append(node.Selections, SelectItem{Expression: FieldRef{Table: node.Joins[0].Source.Name(), Column: "secret"}})
			}
			flags, err := SelectSensitivity(node)
			want := []bool{false, true}
			if len(node.Joins) > 0 {
				want = append(want, true)
			}
			if err != nil || !reflect.DeepEqual(flags, want) {
				t.Fatal(flags, err)
			}
		})
	}
}
