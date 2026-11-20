package expr_test

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/value"
)

func BenchmarkUnifiedExpressions(b *testing.B) {
	type item struct {
		ID    int64
		Name  string
		Count int64
		Note  value.Nullable[string]
	}
	type itemTable struct {
		orm.Table[item, int64]
		Name  orm.Text[item, string]
		Count orm.Number[item, int64]
		Note  orm.NullableText[item, string]
	}
	tbl := orm.Define("expr_bench_items", func(builder orm.Builder[item], m *item) itemTable {
		return itemTable{
			Table: orm.TableOf(builder, &m.ID),
			Name:  orm.TextOf(builder, &m.Name),
			Count: orm.NumberOf(builder, &m.Count),
			Note:  orm.NullableTextOf(builder, &m.Note),
		}
	})
	if err := orm.Check(tbl); err != nil {
		b.Fatal(err)
	}

	b.Run("Value", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = expr.Value(tbl.Name)
		}
	})
	b.Run("TextRow", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = expr.TextRow(tbl.Name)
		}
	})
	b.Run("TextNullableRow", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = expr.TextNullableRow(tbl.Note)
		}
	})
	b.Run("OrderedColumn", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = expr.OrderedColumn(tbl.Count)
		}
	})
	b.Run("CompareColumn", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = expr.CompareColumn(tbl.Name)
		}
	})
	b.Run("Sum", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = expr.Sum(tbl.Count)
		}
	})
	b.Run("Param", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = expr.Param(tbl.Count, int64(7))
		}
	})
}
