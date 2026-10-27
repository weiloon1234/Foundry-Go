package goway_test

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"

	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	ormrows "github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func TestStage6a4RawFragmentsOpaque(t *testing.T) {
	d, err := database.NewStatementDetector(database.StatementDetectorConfig{Threshold: 1, MaxShapes: 16})
	if err != nil {
		t.Fatal(err)
	}
	ctx, scope := d.Begin(t.Context())
	g := &goldenCapture{detector: d}
	db := g.open(ctx)
	defer db.Close(context.Background())
	for range 2 {
		_, err := models.Orders.Query().Where(orm.WhereRaw[models.Order]("total_cents > ? /* private_sql_canary */", 1)).Count(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
	}
	report := scope.Report()
	if len(report.Violations) != 1 || !strings.HasPrefix(report.Violations[0].Shape, "untrusted statement ") || strings.Contains(report.Violations[0].Shape, "private_sql_canary") {
		t.Fatal(report)
	}
	// Selected fragments also invalidate the whole statement's trusted shape.
	ctx, scope = d.Begin(t.Context())
	g.scriptedColumns = []string{"count"}
	g.scriptedBatches = [][][]driver.Value{{{int64(1)}}, {{int64(1)}}}
	type result struct{ Count int64 }
	type fields struct {
		ormrows.Row[result]
		Count ormrows.Field[result, int64]
	}
	schema := ormrows.Define(func(b ormrows.Builder[result], r *result) fields {
		return fields{b.Row(), ormrows.NumberOf(b, &r.Count)}
	})
	q := ormrows.Select(ormrows.FromModel(models.Orders.Query()), schema, ormrows.MapSelected(schema.Count, expr.Raw[models.Order, int64]("COUNT(*)")))
	for range 2 {
		if _, err := ormrows.Read(q).All(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	report = scope.Report()
	if len(report.Violations) != 1 || !strings.HasPrefix(report.Violations[0].Shape, "untrusted statement ") {
		t.Fatal(report)
	}
}
