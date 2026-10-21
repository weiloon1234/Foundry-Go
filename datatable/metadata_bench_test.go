package datatable

import "testing"

func BenchmarkTableRequestPreparation(b *testing.B) {
	table := Define(reportSpec())
	if err := table.Validate(); err != nil {
		b.Fatal(err)
	}
	leaf := Filter{Column: "id", Op: Equal, Values: []string{"7"}}
	nested := leaf
	for range 4 {
		nested = Filter{Op: All, Children: []Filter{nested, {Column: "name", Op: Contains, Values: []string{"member"}}}}
	}
	for _, test := range []struct {
		name    string
		request Request
	}{
		{"shallow", Request{Filters: []Filter{leaf}}},
		{"nested", Request{Filters: []Filter{nested}, Search: "member"}},
	} {
		b.Run(test.name, func(b *testing.B) {
			config := DefaultConfig()
			b.ReportAllocs()
			for b.Loop() {
				prepared, err := table.definition.prepare(test.request, config)
				if err != nil {
					b.Fatal(err)
				}
				if prepared.page.Size != 20 || len(prepared.orders) != 2 || len(prepared.condition.where) == 0 {
					b.Fatal("request preparation omitted its page, orders or predicate")
				}
			}
		})
	}
}
