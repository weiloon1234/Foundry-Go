package scalarqueries_test

import (
	"strings"
	"testing"

	"foundry.test/consumer/scalarqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/datatable"
)

func TestCollatedTextComposition(t *testing.T) {
	f := scalarqueries.Samples
	// doc-example:begin collated-ordering
	name := expr.Collate(expr.Value(orm.TextExpr(f.Name)), expr.CollationUnicode)
	query := expr.OrderBy(f.Query(), expr.Asc(name))
	// doc-example:end collated-ordering
	if _, err := rows.CompileRead(rows.Read(rows.FromModel(query))); err != nil {
		t.Fatal(err)
	}
	var sortOption datatable.ColumnOption[scalarqueries.Sample, scalarqueries.Label] = datatable.SortBy(expr.AsSelected(name))
	_ = sortOption
	note := expr.CollateNullable(expr.Value(orm.NullableTextExpr(f.Note)), expr.ICUCollation("en-US"))
	for _, order := range []expr.Order[scalarqueries.Sample]{expr.Asc(name), expr.Desc(name), expr.Asc(note), expr.Desc(note)} {
		q := expr.OrderBy(expr.Where(f.Query(), expr.Gt(name, "adam"), expr.TextLike(name, "b%"), expr.NullableTextContains(note, "É")), order)
		stmt, err := rows.CompileRead(rows.Read(rows.FromModel(q)))
		if err != nil || !strings.Contains(stmt.SQL(), `COLLATE "und-x-icu"`) || !strings.Contains(stmt.SQL(), `COLLATE "en-US-x-icu"`) {
			t.Fatal("row collation composition", stmt.SQL(), err)
		}
	}
	selected := expr.CollateValue(expr.AsSelected(expr.Row(expr.Value(orm.TextExpr(f.Name)))), expr.CollationC)
	nullableSelected := expr.CollateNullableValue(expr.AsSelected(expr.Row(expr.Value(orm.NullableTextExpr(f.Note)))), expr.CollationC)
	for _, order := range []expr.ProjectionOrder[scalarqueries.Sample]{expr.AscValue(selected), expr.DescValue(selected), expr.AscValue(nullableSelected), expr.DescValue(nullableSelected)} {
		q := rows.OrderScalar(rows.SelectScalar(rows.From(f.Query()), selected), order)
		if _, err := rows.CompileRead(rows.ReadScalar(q)); err != nil {
			t.Fatal(err)
		}
	}
	q := rows.GroupByScalar(rows.SelectScalar(rows.From(f.Query()), selected), expr.Group(expr.Value(orm.TextExpr(f.Name))))
	q = rows.HavingScalar(q, expr.GtValue(expr.OrderValue(selected), "adam"), expr.TextLikeValue(expr.TextValue(selected), "b%"))
	if _, err := rows.CompileRead(rows.ReadScalar(q)); err != nil {
		t.Fatal("selected comparison lost collation", err)
	}
	minimum := expr.CollateNullableValue(expr.AsSelected(expr.Min(orm.TextExpr(f.Name))), expr.CollationUnicode)
	if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(rows.From(f.Query()), minimum))); err != nil {
		t.Fatal("selected aggregate lost collation", err)
	}
}
