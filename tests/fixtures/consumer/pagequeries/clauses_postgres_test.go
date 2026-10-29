package pagequeries_test

import (
	"reflect"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestPostgresPagesRetainRelationalSemantics(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		o, totals := models.Orders, reports.BuyerTotalRows
		grouped := rows.OrderBy(rows.Having(rows.GroupBy(rows.Select(rows.From(o.Query()), totals.Row,
			rows.Map(totals.BuyerID, expr.Value(orm.ColumnExpr(o.BuyerID))), rows.MapSelected(totals.Total, expr.Sum(orm.NumberExpr(o.TotalCents)).Value()), rows.MapSelected(totals.Orders, expr.Count[models.Order]().Value()), rows.MapSelected(totals.Average, expr.Avg(orm.NumberExpr(o.TotalCents)).Value())), expr.Group(expr.Value(orm.ColumnExpr(o.BuyerID)))), expr.Count[models.Order]().Gt(0)), expr.Value(orm.ColumnExpr(o.BuyerID)).Asc())
		p, err := rows.Read(grouped).Paginate(t.Context(), tx, orm.PageRequest{Number: 1, Size: 1})
		if err != nil {
			return err
		}
		if p.Total != 2 || p.Pages != 2 || len(p.Items) != 1 {
			t.Fatal("pagination counted input rows instead of groups")
		}
		v, valid := p.Items[0].Total.Get()
		if !valid || v.String() != "3" {
			t.Fatal("page lost exact aggregate codec")
		}
		definition := rows.CTE("paged_reports", rows.RecordOf(rows.Limit(summaries(), 2)))
		a := rows.AsRecord[reportAlias](definition, "report")
		id := rows.At(rows.Scope(a), reports.UserSummaries.ID)
		derived := rows.SelectRecord(a, rows.Scope(a)).OrderBy(id.Asc())
		d, err := rows.ReadRecord(derived).Paginate(t.Context(), tx, orm.PageRequest{Number: 1, Size: 1})
		if err != nil || d.Total != 2 {
			t.Fatal("page discarded nested CTE limit", err)
		}
		combined := rows.UnionAll(rows.RecordOf(rows.Limit(summaries(), 1)), rows.RecordOf(rows.Limit(summaries(), 2)))
		email := rows.At(combined.Scope(), reports.UserSummaries.Email)
		combined = combined.OrderBy(email.Asc())
		setPage, err := rows.ReadSet(combined).Paginate(t.Context(), tx, orm.PageRequest{Number: 2, Size: 2})
		if err != nil || setPage.Total != 3 || len(setPage.Items) != 1 {
			t.Fatal("set page lost multiplicity/input limits", err)
		}
		if simple, err := rows.ReadSet(combined).SimplePaginate(t.Context(), tx, orm.PageRequest{Number: 1, Size: 2}); err != nil || !simple.HasMore {
			t.Fatal("set simple page failed", err)
		}
		values := rows.OrderScalar(rows.DistinctScalar(rows.SelectScalar(rows.From(models.Users.Query()), expr.Row(expr.Value(orm.NullableTextExpr(models.Users.Nickname))).Value())), expr.Value(orm.NullableTextExpr(models.Users.Nickname)).Asc())
		vp, err := rows.ReadScalar(values).Paginate(t.Context(), tx, orm.PageRequest{Number: 1, Size: 1})
		if err != nil || vp.Total != 2 || len(vp.Items) != 1 || vp.Items[0] != value.Of("Bee") {
			t.Fatal("distinct/nullable value page failed", err)
		}
		vs := rows.UnionValues(rows.Values(values), rows.Values(values))
		vs = vs.OrderBy(vs.Value().Asc())
		if p, err := rows.ReadValues(vs).SimplePaginate(t.Context(), tx, orm.PageRequest{Number: 1, Size: 1}); err != nil || !p.HasMore {
			t.Fatal("combined value simple page failed", err)
		}
		if p, err := rows.ReadValues(vs).Paginate(t.Context(), tx, orm.PageRequest{Number: 1, Size: 1}); err != nil || p.Total != 2 {
			t.Fatal("combined value page failed", err)
		}
		window := expr.WindowFor(rows.ModelScope(models.Users.Query())).OrderBy(expr.Value(orm.TextExpr(models.Users.Email)).Asc())
		position := expr.RowNumber(window)
		if p, err := rows.ReadScalar(rows.OrderScalar(rows.SelectScalar(rows.From(models.Users.Query()), position), position.Asc())).Paginate(t.Context(), tx, orm.PageRequest{Number: 2, Size: 1}); err != nil || p.Total != 3 || !reflect.DeepEqual(p.Items, []int64{2}) {
			t.Fatal("window page evaluated after limit", err)
		}
		buyers := rows.As[buyerAlias](models.Users.Query(), "buyer")
		orders := rows.As[orderAlias](models.Orders.Query(), "purchase")
		joined := rows.InnerJoin(buyers, orders, rows.On(expr.At(rows.Scope(buyers), orm.KeyExpr(models.Users.Key())), expr.At(rows.Scope(orders), orm.ColumnExpr(models.Orders.BuyerID))))
		left, right := rows.LeftScope(joined, rows.Scope(buyers)), rows.RightScope(joined, rows.Scope(orders))
		jq := rows.SelectRecord(joined, left).OrderBy(expr.At(left, orm.TextExpr(models.Users.Email)).Asc(), expr.At(right, orm.KeyExpr(models.Orders.Key())).Asc())
		if p, err := rows.ReadRecord(jq).Paginate(t.Context(), tx, orm.PageRequest{Number: 1, Size: 1}); err != nil || p.Total != 3 || p.Items[0].ID != users[0].ID {
			t.Fatal("join page lost multiplicity", err)
		}
		return nil
	})
}
