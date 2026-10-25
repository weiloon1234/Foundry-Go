package goway_test

import (
	"database/sql/driver"
	"fmt"
	"foundry.test/goway/models"
	old "foundry.test/goway/oldmodels"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/database/query"
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
				for _, legacy := range []bool{true, false} {
					g := &goldenCapture{scriptedColumns: []string{"amount"}, scriptedBatches: [][][]driver.Value{{{int64(500)}, {int64(600)}}}}
					if operation == "count" {
						g.scriptedBatches = [][][]driver.Value{{{int64(2)}}}
					}
					if operation == "page" {
						g.scriptedBatches = [][][]driver.Value{{{int64(2)}}, {{int64(500)}}}
					}
					db := g.open(t.Context())
					defer db.Close(t.Context())
					if legacy {
						f := old.OrderFields()
						out := query.NewProjectionField[reportAmount, int64]("amount")
						definition := query.DefineProjection([]query.ProjectionColumn[reportAmount]{out.Column()}, func(row database.Row) (reportAmount, error) {
							var r reportAmount
							err := row.Scan(&r.Amount)
							return r, err
						})
						q := query.Project(old.QueryOrders(), definition, query.Map(out, query.Value(f.TotalCents))).Where(f.TotalCents.Gt(100)).OrderBy(f.TotalCents.Desc())
						switch shape {
						case "group":
							q = q.GroupBy(f.TotalCents.Group()).Having(query.Count[old.Order]().Gt(0))
						case "distinct":
							q = q.Distinct()
						case "window":
							q = q.Limit(2).Offset(1)
						case "distinct_on":
							q = q.DistinctOn(f.TotalCents.Group())
						}
						if shape != "window" && shape != "distinct_on" {
							q = q.ReorderUnwindowed(f.TotalCents.Asc())
						}
						switch operation {
						case "compile":
							s := must(q.Compile())
							fmt.Fprintln(&g.output, oracleSnapshot([]any{s.SQL(), s.Arguments()}))
						case "count":
							fmt.Fprintln(&g.output, oracleSnapshot([]any{must(q.Count(t.Context(), db))}))
						case "each":
							if err := q.Each(t.Context(), db, func(r reportAmount) error { fmt.Fprintln(&g.output, oracleSnapshot([]any{r})); return nil }); err != nil {
								t.Fatal(err)
							}
						case "page":
							p, err := q.Paginate(t.Context(), db, orm.Page(1, 1))
							fmt.Fprintln(&g.output, oracleSnapshot([]any{p, err != nil}))
						case "simple":
							p, err := q.SimplePaginate(t.Context(), db, orm.Page(1, 1))
							fmt.Fprintln(&g.output, oracleSnapshot([]any{p, err != nil}))
						}
					} else {
						f := expr.Value(models.Orders.TotalCents)
						q := rowset.Select(rowset.From(models.Orders.Query()), reportSchema, rowset.Map(reportSchema.Amount, f)).Where(expr.ModelPredicate(models.Orders.TotalCents.Gt(100))).OrderBy(f.Desc())
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
							s := must(rowset.Compile(q))
							fmt.Fprintln(&g.output, oracleSnapshot([]any{s.SQL(), s.Arguments()}))
						case "count":
							fmt.Fprintln(&g.output, oracleSnapshot([]any{must(rowset.Count(t.Context(), db, q))}))
						case "each":
							if err := rowset.Each(t.Context(), db, q, func(r reportAmount) error { fmt.Fprintln(&g.output, oracleSnapshot([]any{r})); return nil }); err != nil {
								t.Fatal(err)
							}
						case "page":
							p, err := rowset.Paginate(t.Context(), db, q, orm.Page(1, 1))
							fmt.Fprintln(&g.output, oracleSnapshot([]any{p, err != nil}))
						case "simple":
							p, err := rowset.SimplePaginate(t.Context(), db, q, orm.Page(1, 1))
							fmt.Fprintln(&g.output, oracleSnapshot([]any{p, err != nil}))
						}
					}
					transcripts = append(transcripts, g.output.String())
				}
				if !oracleEqual(t, "report_projection_golden/1", transcripts[0], transcripts[1]) {
					t.Fatal(goldenDiff(transcripts[0], transcripts[1]))
				}
			})
		}
	}
}

func TestReportFilterAdaptersGolden(t *testing.T) {
	oldF := old.UserFields()
	text := expr.TextNullableRow(models.Users.Nickname)
	enum := expr.CompareRow(expr.Value(models.Users.Status))
	oldPs := []query.Predicate[old.User]{
		query.Or(oldF.Nickname.Eq("Ada").Not(), oldF.Nickname.IsNull()),
		query.Or(oldF.Nickname.Ne("Ada"), oldF.Nickname.IsNull()),
		query.Or(oldF.Nickname.In("Ada", "Bob").Not(), oldF.Nickname.IsNull()),
		oldF.Nickname.Contains(`A%_`), oldF.Nickname.IContains(`A%_`), oldF.Nickname.Like("A%"),
		oldF.Status.In(old.StatusActive, old.StatusDisabled),
		oldF.Nickname.Eq("Ada"),
		oldF.Email.Contains("Ada"), oldF.Email.Like("A%"), oldF.Email.Gt("Ada"), oldF.Nickname.Gt("Ada"),
		old.UserRelations().Orders.Where(old.OrderFields().TotalCents.Gt(100)).Exists(),
	}
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
	for i, p := range oldPs {
		a := must(old.QueryUsers().Where(p).Compile())
		b := must(rowset.Compile(rowset.FromModel(models.Users.Query()).Where(newPs[i])))
		before, after := oracleSnapshot([]any{a.SQL(), a.Arguments()}), oracleSnapshot([]any{b.SQL(), b.Arguments()})
		if !oracleEqual(t, "report_projection_golden/filters", before, after) {
			t.Fatal(goldenDiff(before, after))
		}
	}
}
