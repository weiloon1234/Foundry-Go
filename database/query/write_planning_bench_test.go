package query

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
)

// These compile real typed plans without a database. Use the same source and
// prebuilt test binaries when comparing allocation and runtime costs.
func BenchmarkSharedWritePlanning(b *testing.B) {
	q := mutationQuery()
	name := NewTextField[user, string]("users", "name", codec.String[string]())
	note := NewNullableTextField[user, string]("users", "note", codec.String[string]())
	policy := OnConflictKeys(Lower(name).Group()).TargetWhere(name.Ne("")).DoUpdate(note.Set("updated"))
	upsert := insertPlan[user]{query: q, rows: []Mutation[user]{Change(Assign[user]("users", "name", codec.String[string](), "first"))}, conflict: &policy}
	source, destination, key, mapping := sourceWriteFixture()
	input := NewTextField[insertSource, string]("source_records", "name", codec.String[string]())
	output := NewTextField[insertArchive, string]("archives", "name", codec.String[string]())
	insertion := InsertFrom(source.Limit(3), destination, MapInsert(output, Value(input))).Values(Change(Assign[insertArchive]("archives", "tag", codec.String[string](), "fixed")))
	update := UpdateFrom(destination, source.Limit(3)).Match(key).Select(mapping)
	for _, scenario := range []struct {
		name    string
		compile func() (Statement, error)
	}{
		{"Upsert", upsert.compile},
		{"InsertSelect", func() (Statement, error) { return insertion.compile(true) }},
		{"UpdateFrom", func() (Statement, error) { return update.plan.compile(true) }},
	} {
		b.Run(scenario.name, func(b *testing.B) {
			if _, err := scenario.compile(); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				if _, err := scenario.compile(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
