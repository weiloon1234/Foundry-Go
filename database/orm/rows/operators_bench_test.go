package rows_test

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func BenchmarkUnifiedRowMappings(b *testing.B) {
	type item struct {
		ID    int64
		Name  string
		Count int64
	}
	type itemTable struct {
		orm.Table[item, int64]
		ID    orm.Primary[item, int64]
		Name  orm.Text[item, string]
		Count orm.Number[item, int64]
	}
	tbl := orm.Define("rows_bench_items", func(builder orm.Builder[item], m *item) itemTable {
		t := orm.TableOf(builder, &m.ID)
		return itemTable{
			Table: t,
			ID:    orm.PrimaryOf(builder, t.Key()),
			Name:  orm.TextOf(builder, &m.Name),
			Count: orm.NumberOf(builder, &m.Count),
		}
	})
	type summary struct {
		Name  string
		Count int64
	}
	type summaryRow struct {
		rows.Row[summary]
		Name  rows.Field[summary, string]
		Count rows.Field[summary, int64]
	}
	out := rows.Define(func(builder rows.Builder[summary], r *summary) summaryRow {
		return summaryRow{builder.Row(), rows.TextOf(builder, &r.Name), rows.NumberOf(builder, &r.Count)}
	})

	b.Run("SelectMap", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			q := rows.Select(rows.From(tbl.Query()), out, rows.Map(out.Name, expr.Value(tbl.Name)), rows.Map(out.Count, expr.Value(tbl.Count)))
			if _, err := rows.CompileRead(rows.Read(q)); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("MapInsert", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = rows.MapInsert(tbl.ID, expr.Value(tbl.ID))
			_ = rows.MapInsert(tbl.Name, expr.Value(tbl.Name))
		}
	})
	b.Run("MapUpdate", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = rows.MapUpdate(tbl.Name, expr.Value(tbl.Name))
		}
	})
	b.Run("MatchSource", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = rows.MatchSource(tbl.Key(), expr.Value(tbl.ID))
		}
	})
}
