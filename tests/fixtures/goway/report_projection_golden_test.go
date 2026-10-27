package goway_test

import (
	"database/sql/driver"
	"fmt"
	"foundry.test/goway/models"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"

	"testing"
)

type reportAmount struct{ Amount int64 }
type reportFields struct {
	rowset.Row[reportAmount]
	Amount rowset.Field[reportAmount, int64]
}

var reportSchema = rowset.Define(func(b rowset.Builder[reportAmount], r *reportAmount) reportFields {
	return reportFields{b.Row(), rowset.NumberOf(b, &r.Amount)}
})

func TestReportProjectionGolden(t *testing.T) {
	for _, shape := range []string{"plain", "group", "distinct", "window", "distinct_on"} {
		for _, operation := range []string{"compile", "count", "each", "page", "simple"} {
			t.Run(shape+"/"+operation, func(t *testing.T) {
				var transcripts []string
				{
					g := &goldenCapture{scriptedColumns: []string{"amount"}, scriptedBatches: [][][]driver.Value{{{int64(500)}, {int64(600)}}}}
					if operation == "count" {
						g.scriptedBatches = [][][]driver.Value{{{int64(2)}}}
					}
					if operation == "page" {
						g.scriptedBatches = [][][]driver.Value{{{int64(2)}}, {{int64(500)}}}
					}
					db := g.open(t.Context())
					defer db.Close(t.Context())

					f := expr.Value(models.Orders.TotalCents)
					q := rowset.OrderBy(rowset.Select(rowset.From(models.Orders.Query()), reportSchema, rowset.Map(reportSchema.Amount, f)).Where(expr.ModelPredicate(models.Orders.TotalCents.Gt(100))), f.Desc())
					switch shape {
					case "group":
						q = rowset.Having(rowset.GroupBy(q, expr.Group(f)), expr.Count[models.Order]().Gt(0))
					case "distinct":
						q = rowset.Distinct(q)
					case "window":
						q = rowset.Offset(rowset.Limit(q, 2), 1)
					case "distinct_on":
						q = rowset.DistinctOn(q, expr.Group(f))
					}
					if shape != "window" && shape != "distinct_on" {
						q = rowset.ReorderUnwindowed(q, f.Asc())
					}
					switch operation {
					case "compile":
						s := must(rowset.CompileRead(rowset.Read(q)))
						fmt.Fprintln(&g.output, oracleSnapshot([]any{s.SQL(), s.Arguments()}))
					case "count":
						fmt.Fprintln(&g.output, oracleSnapshot([]any{must(rowset.Read(q).Count(t.Context(), db))}))
					case "each":
						if err := rowset.EachRead(t.Context(), db, rowset.Read(q), func(r reportAmount) error { fmt.Fprintln(&g.output, oracleSnapshot([]any{r})); return nil }); err != nil {
							t.Fatal(err)
						}
					case "page":
						p, err := rowset.Read(q).Paginate(t.Context(), db, orm.Page(1, 1))
						fmt.Fprintln(&g.output, oracleSnapshot([]any{p, err != nil}))
					case "simple":
						p, err := rowset.Read(q).SimplePaginate(t.Context(), db, orm.Page(1, 1))
						fmt.Fprintln(&g.output, oracleSnapshot([]any{p, err != nil}))
					}

					transcripts = append(transcripts, g.output.String())
				}
				if !oracleEqual(t, "report_projection_golden/1", transcripts[0]) {
					t.Fatal("frozen legacy comparison differs")
				}
			})
		}
	}
}

func TestReportFilterAdaptersGolden(t *testing.T) {
	text := expr.TextNullableRow(models.Users.Nickname)
	enum := expr.CompareRow(expr.Value(models.Users.Status))

	newPs := []expr.Predicate[models.User]{
		expr.Or(expr.Not(text.Eq("Ada")), text.IsNull()),
		expr.Or(text.Ne("Ada"), text.IsNull()),
		expr.Or(expr.Not(text.In("Ada", "Bob")), text.IsNull()),
		text.Contains(`A%_`), text.IContains(`A%_`), text.Like("A%"),
		enum.In(models.StatusActive, models.StatusDisabled),
		expr.CompareNullableRow(expr.Value(models.Users.Nickname)).Eq("Ada"),
		expr.TextRow(models.Users.Email).Contains("Ada"), expr.TextRow(models.Users.Email).Like("A%"), expr.TextRow(models.Users.Email).Gt("Ada"), text.Gt("Ada"),
		expr.Has(models.UserRelations.Orders, expr.ModelPredicate(models.Orders.TotalCents.Gt(100))),
	}

	for _, p := range newPs {
		statement := must(rowset.CompileRead(rowset.Read(rowset.FromModel(models.Users.Query()).Where(p))))
		if !oracleEqual(t, "report_projection_golden/filters", oracleSnapshot([]any{statement.SQL(), statement.Arguments()})) {
			t.Fatal("frozen predicate differs")
		}
	}
}
