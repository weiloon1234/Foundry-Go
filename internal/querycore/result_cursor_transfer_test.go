package querycore

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/fault"
	"strings"
	"testing"
)

func TestResultCursorCountsAppendedIdentityAgainstFieldBound(t *testing.T) {
	columns := []Column{{Name: "id"}}
	var orders []QueryOrder
	for i := range MaxCursorFields {
		name := fmt.Sprintf("sort_%d", i)
		columns = append(columns, Column{Name: name})
		orders = append(orders, QueryOrder{Field: FieldRef{Table: "result", Column: name}})
	}
	node := SelectNode{Source: TableSource{Query: &SelectNode{}, Alias: "result"}}
	if _, err := ResultCursorOrders(node, columns, orders, []FieldRef{{Table: "result", Column: "id"}}); !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "field bound") {
		t.Fatal(err)
	}
}

func TestResultCursorRejectsAmbiguousDeclarations(t *testing.T) {
	columns := []Column{{Name: "id"}, {Name: "rank", Nullable: true}}
	node := SelectNode{Source: TableSource{Query: &SelectNode{Source: TableSource{Table: "records", Columns: columns}, Selections: ColumnSelections("records", columns)}, Alias: "result", Columns: columns}, Selections: ColumnSelections("result", columns)}
	id := FieldRef{Table: "result", Column: "id"}
	rank := FieldRef{Table: "result", Column: "rank"}
	for _, keys := range [][]FieldRef{nil, {id, id}, {{Table: "result", Column: "missing"}}, {{Table: "foreign", Column: "id"}}, {{}}, make([]FieldRef, MaxCursorFields+1)} {
		if _, err := ResultCursorOrders(node, columns, nil, keys); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid identity accepted", err)
		}
	}
	for _, orders := range [][]QueryOrder{{{Field: id}, {Field: id}}, {{Field: FieldRef{Table: "foreign", Column: "id"}}}, {{Field: FieldRef{Table: "result", Column: "missing"}}}, {{Field: id, Computed: &ComputedOrder{Expression: AggregateNode{Kind: CountAll}}}}, make([]QueryOrder, MaxCursorFields+1)} {
		if _, err := ResultCursorOrders(node, columns, orders, []FieldRef{id}); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid ordering accepted", err)
		}
	}
	orders, err := ResultCursorOrders(node, columns, []QueryOrder{{Field: rank}}, []FieldRef{id})
	if err != nil {
		t.Fatal(err)
	}
	good := []ResultCursorField{{Name: "id", Valid: true}, {Name: "rank", Valid: true}}
	indices, required, err := ResultCursorFields(node, columns, orders, good)
	if err != nil || len(indices) != 2 || indices[0] != 1 || indices[1] != 0 || required[0] || !required[1] {
		t.Fatal(indices, required, err)
	}
	for _, fields := range [][]ResultCursorField{nil, {{Name: "id", Valid: true}}, {{Name: "id", Valid: true}, {Name: "id", Valid: true}}, {{Name: "missing", Valid: true}}, {{Name: "rank", Valid: false}}, {{Name: "id", Valid: true}, {Name: "rank", Valid: true, Sensitive: true}}} {
		if _, _, err := ResultCursorFields(node, columns, orders, fields); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid getter accepted", err)
		}
	}
}

func TestNullableCursorTerminalBoundaryCompilesFalse(t *testing.T) {
	field := FieldRef{Table: "records", Column: "rank"}
	node := SelectNode{Source: TableSource{Table: "records", Columns: []Column{{Name: "rank", Nullable: true}}}, Selections: []SelectItem{{Expression: field}}}
	node.Predicates = []Expression{CursorPredicate([]QueryOrder{{Field: field}}, []driver.Value{nil}, func(QueryOrder) bool { return true })}
	s, err := CompileRead(node, nil, false, nil, true)
	if err != nil || !strings.Contains(s.SQLText, "WHERE FALSE") || len(s.Bindings) != 0 {
		t.Fatal(s.SQLText, err)
	}
}
