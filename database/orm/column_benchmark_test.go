package orm

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// BenchmarkInsertCompile measures the private compile boundary. The independent
// consumer's identically named benchmark cannot access it and deliberately skips.
func BenchmarkInsertCompile(b *testing.B) {
	type item struct {
		ID     int64
		Name   string
		Amount int64
	}
	type fields struct {
		Table[item, int64]
		Name   Text[item, string]
		Amount Number[item, int64]
	}
	table := Define("items", func(builder Builder[item], m *item) fields {
		return fields{TableOf(builder, &m.ID), TextOf(builder, &m.Name), NumberOf(builder, &m.Amount)}
	})
	if err := Check(table); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		plan, err := planWrite(newQuery(table.core), querycore.InsertModel, []Assignment[item]{table.Key().Set(1), table.Name.Set("item"), table.Amount.Set(10)}, false)
		if err != nil {
			b.Fatal(err)
		}
		statement, err := plan.compile()
		if err != nil || len(statement.Bindings) != 3 || len(statement.SQLText) == 0 {
			b.Fatalf("compile: %v", err)
		}
	}
}
