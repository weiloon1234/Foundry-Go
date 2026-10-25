package datatable

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func TestDatatableCutoverProjectionGolden(t *testing.T) {
	for _, tc := range []struct {
		name    string
		request Request
	}{
		{"scoped", Request{}},
		{"search_literal", Request{Search: "A%_!"}},
		{"nullable_ne", Request{Filters: []Filter{{Op: NotEqual, Column: "note", Values: []string{"memo"}}}}},
		{"nullable_not", Request{Filters: []Filter{{Op: Not, Children: []Filter{{Op: Equal, Column: "note", Values: []string{"memo"}}}}}}},
		{"exact_between", Request{Filters: []Filter{{Op: Between, Column: "amount", Values: []string{"1.25", "9.50"}}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			table := Define(reportSpec())
			p, err := table.definition.prepare(tc.request, DefaultConfig())
			if err != nil {
				t.Fatal(err)
			}
			source, err := table.definition.scoped(t.Context(), reportActor{Tenant: 7, Allowed: true}, QueryAction, p)
			if err != nil {
				t.Fatal(err)
			}
			got, err := rows.Compile(source.rows)
			if err != nil {
				t.Fatal(err)
			}
			frozenMatch(t, "projection", []any{got.SQL(), got.Arguments()})
		})
	}
}
