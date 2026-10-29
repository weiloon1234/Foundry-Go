package orm

import (
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
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
		plan, err := planWrite(newQuery(table.core), querycore.InsertModel, []Assignment[item]{KeySet(table.Key(), 1), TextSet(table.Name, "item"), NumberSet(table.Amount, 10)}, false)
		if err != nil {
			b.Fatal(err)
		}
		statement, err := plan.compile()
		if err != nil || len(statement.Bindings) != 3 || len(statement.SQLText) == 0 {
			b.Fatalf("compile: %v", err)
		}
	}
}

// BenchmarkSharedWritePlanning retains the three legacy compiler workloads.
func BenchmarkSharedWritePlanning(b *testing.B) {
	source, dest := sourceTransferTables()
	mapping := FoundryInsertMapping[transferSource, transferArchive](transferMapping(TextExpr(dest.Name), TextExpr(source.Name)), sqlowner.Expressions())
	input := transferSourceMetadata(source.Query().Limit(3))
	insertion := FoundryInsertFrom[transferSource](dest.Query(), input, sqlowner.Queries()).Select(mapping).Values(TextSet(dest.Tag, "fixed"))
	key := FoundrySourceKey[transferSource, transferArchive](transferMapping(KeyExpr(dest.Key()), KeyExpr(source.Key())), sqlowner.Expressions())
	update := FoundryUpdateFrom[transferSource](dest.Query(), input, sqlowner.Queries()).Match(key).Select(FoundryUpdateMapping[transferSource, transferArchive](transferMapping(TextExpr(dest.Name), TextExpr(source.Name)), sqlowner.Expressions()))
	p, e := planWrite(dest.Query().state, querycore.InsertModel, []Assignment[transferArchive]{TextSet(dest.Name, "first"), TextSet(dest.Tag, "fixed")}, false)
	if e != nil {
		b.Fatal(e)
	}
	policy := OnConflict(ConflictKey(TextExpr(dest.Name))).DoUpdate(ConflictSet(NullableTextExpr(dest.Note), "updated"))
	p, e = p.withConflict(&policy.state)
	if e != nil {
		b.Fatal(e)
	}
	p.batch = [][]querycore.MutationAssignment{p.assignments}
	p.assignments = nil
	for _, x := range []struct {
		name    string
		compile func() (querycore.Statement, error)
	}{{"Upsert", p.compile}, {"InsertSelect", func() (querycore.Statement, error) { return insertion.plan.compile(true) }}, {"UpdateFrom", func() (querycore.Statement, error) { return update.plan.compile(true) }}} {
		b.Run(x.name, func(b *testing.B) {
			if _, e := x.compile(); e != nil {
				b.Fatal(e)
			}
			b.ReportAllocs()
			for b.Loop() {
				if _, e := x.compile(); e != nil {
					b.Fatal(e)
				}
			}
		})
	}
}

// BenchmarkUpdateCompile includes planning, validation, binding and SQL rendering.
func BenchmarkUpdateCompile(b *testing.B) {
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
		plan, err := planWrite(table.Query().Where(KeyEq(table.Key(), 1)).state, querycore.UpdateModel, []Assignment[item]{NumberSet(table.Amount, 10), TextSet(table.Name, "item")}, false)
		if err != nil {
			b.Fatal(err)
		}
		statement, err := plan.compile()
		if err != nil || len(statement.Bindings) != 3 || len(statement.SQLText) == 0 {
			b.Fatalf("compile: %v", err)
		}
	}
}
