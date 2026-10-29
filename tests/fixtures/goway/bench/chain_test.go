package bench

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"testing"

	"foundry.test/goway/models"
)

var (
	sinkQuery orm.Query[models.Order]
	sinkStmt  any
	sinkErr   error
)

func BenchmarkChainCompile(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		stmt, err := orm.Compile(models.Orders.Query().
			Where(orm.NumberGt(models.Orders.TotalCents, 100)).
			OrderBy(orm.KeyDesc(models.Orders.ID)).
			Limit(10).
			Offset(5))
		if err != nil {
			b.Fatal(err)
		}
		sinkStmt = stmt
		sinkErr = err
	}
}

func BenchmarkChainBuild(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q := models.Orders.Query().
			Where(orm.NumberGt(models.Orders.TotalCents, 100)).
			OrderBy(orm.KeyDesc(models.Orders.ID)).
			Limit(10).
			Offset(5)
		sinkQuery = q
	}
}
