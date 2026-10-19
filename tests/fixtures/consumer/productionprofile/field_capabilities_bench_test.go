package productionprofile_test

import (
	"testing"

	"foundry.test/consumer/productionprofile"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/decimal"
)

var selectedField query.Expression[productionprofile.Record, string]
var parameterField query.RowExpression[productionprofile.Record, int64]
var summarizedField query.NullableOrderedAggregate[productionprofile.Record, decimal.Decimal]

func BenchmarkProfileFieldSelection(b *testing.B) {
	f := productionprofile.RecordFields().Name
	b.ReportAllocs()
	for b.Loop() {
		selectedField = query.Value(f)
	}
}
func BenchmarkProfileFieldParameter(b *testing.B) {
	f := productionprofile.RecordFields().Score
	b.ReportAllocs()
	for b.Loop() {
		parameterField = query.Param(f, int64(1))
	}
}
func BenchmarkProfileFieldSummary(b *testing.B) {
	f := productionprofile.RecordFields().Score
	b.ReportAllocs()
	for b.Loop() {
		summarizedField = query.Sum(f)
	}
}
