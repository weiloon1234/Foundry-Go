package productionprofile_test

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"testing"

	"foundry.test/consumer/productionprofile"

	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/decimal"
)

var selectedField expr.Selected[productionprofile.Record, string]
var parameterField expr.RowExpression[productionprofile.Record, int64]
var summarizedField expr.NullableOrderedAggregate[productionprofile.Record, decimal.Decimal]

func BenchmarkProfileFieldSelection(b *testing.B) {
	f := productionprofile.Records.Name
	b.ReportAllocs()
	for b.Loop() {
		selectedField = expr.Row(expr.Value(orm.TextExpr(f))).Value()
	}
}
func BenchmarkProfileFieldParameter(b *testing.B) {
	f := productionprofile.Records.Score
	b.ReportAllocs()
	for b.Loop() {
		parameterField = expr.Param(orm.NumberExpr(f), int64(1))
	}
}
func BenchmarkProfileFieldSummary(b *testing.B) {
	f := productionprofile.Records.Score
	b.ReportAllocs()
	for b.Loop() {
		summarizedField = expr.Sum(orm.NumberExpr(f))
	}
}
