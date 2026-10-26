package advancedqueries_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type totalsAlias struct{}
type reportAlias struct{}
type buyerAlias struct{}

func TestPostgresTypedSubqueries(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		if err := checkDerivedReports(t, tx, users); err != nil {
			return err
		}
		return checkSubqueryValues(t, tx, users)
	})
}

func checkDerivedReports(t *testing.T, tx *database.Tx, users []models.User) error {
	o := models.Orders
	totals := rowset.GroupBy(rowset.Select(rowset.From(models.Orders.Query()), reports.BuyerTotalRows.Row,
		rowset.Map(reports.BuyerTotalRows.BuyerID, expr.Value(o.BuyerID)),
		rowset.MapSelected(reports.BuyerTotalRows.Total, expr.Sum(o.TotalCents).Value()),
		rowset.MapSelected(reports.BuyerTotalRows.Orders, expr.Count[models.Order]().Value()),
		rowset.MapSelected(reports.BuyerTotalRows.Average, expr.Avg(o.TotalCents).Value()),
	), expr.Group(expr.Value(o.BuyerID)))
	// Grouping, HAVING and a selected window remain inside the derived source.
	source := rowset.AsRecord[totalsAlias](rowset.RecordOf(rowset.Limit(rowset.OrderBy(rowset.Having(totals, expr.Count[models.Order]().Gte(2)), expr.Count[models.Order]().Desc()), 1)), "totals")
	fields := rowset.Scope(source)
	values := rowset.WhereScalar(rowset.SelectScalar(source, expr.Row(rowset.At(fields, reports.BuyerTotalRows.Total)).Value()), expr.OrderNullableRow(rowset.At(fields, reports.BuyerTotalRows.Total)).Gt(decimal.FromInt64(2)))
	amounts, err := rowset.ReadScalar(values).All(t.Context(), tx)
	if err != nil {
		return err
	}
	if len(amounts) != 1 {
		t.Fatal("derived grouping/HAVING/window did not retain the expected group")
	}
	if amount, ok := amounts[0].Get(); !ok || amount.String() != "3" {
		t.Error("derived decimal value changed")
	}
	// An outer join makes the non-null count nullable and keeps SUM singly nullable.
	u := rowset.As[buyerAlias](models.Users.Query(), "buyer")
	joined := rowset.LeftJoin(u, source, rowset.OnEqual(expr.At(rowset.Scope(u), models.Users.ID), rowset.At(fields, reports.BuyerTotalRows.BuyerID)))
	buyer := rowset.LeftScope(joined, rowset.Scope(u))
	stats := rowset.NullableRightScope(joined, rowset.Scope(source))
	report := rowset.Select(joined, reports.UserOrderStatsRows.Row,
		rowset.Map(reports.UserOrderStatsRows.ID, expr.At(buyer, models.Users.ID)),
		rowset.Map(reports.UserOrderStatsRows.Email, expr.At(buyer, models.Users.Email)),
		rowset.MapSelected(reports.UserOrderStatsRows.Total, expr.Row(rowset.AtNullableValue(stats, reports.BuyerTotalRows.Total)).Value()),
		rowset.MapSelected(reports.UserOrderStatsRows.Orders, expr.Row(rowset.AtNullable(stats, reports.BuyerTotalRows.Orders)).Value()),
	).OrderBy(expr.At(buyer, models.Users.Email).Asc())
	rows, err := report.All(t.Context(), tx)
	if err != nil {
		return err
	}
	if len(rows) != 3 {
		t.Fatal("derived join discarded unmatched users")
	}
	if count, ok := rows[0].Orders.Get(); !ok || count != 2 {
		t.Error("derived group count changed")
	}
	if !rows[1].Orders.IsNull() || !rows[2].Total.IsNull() {
		t.Error("derived outer join lost result nullability")
	}
	// A complete joined result can itself be the right source of another join.
	derived := rowset.AsRecord[reportAlias](rowset.RecordOf(report), "report")
	df := rowset.Scope(derived)
	again := rowset.InnerJoin(u, derived, rowset.OnEqual(expr.At(rowset.Scope(u), models.Users.ID), rowset.At(df, reports.UserOrderStatsRows.ID)))
	joinedFields := rowset.RightScope(again, rowset.Scope(derived))
	if emails, err := rowset.ReadScalar(rowset.OrderScalar(rowset.SelectScalar(again, expr.Row(rowset.At(joinedFields, reports.UserOrderStatsRows.Email)).Value()), rowset.At(joinedFields, reports.UserOrderStatsRows.Email).Asc())).All(t.Context(), tx); err != nil || len(emails) != 3 || emails[0] != users[0].Email {
		t.Error("declared joined result could not be joined again", err)
	}
	// Derived enum fields retain enum operators and decoders, including output aliases.
	base := models.Users
	summary := rowset.Select(rowset.From(models.Users.Query()), reports.UserSummaries.Row,
		rowset.Map(reports.UserSummaries.ID, expr.Value(base.ID)),
		rowset.Map(reports.UserSummaries.Email, expr.Value(base.Email)),
		rowset.Map(reports.UserSummaries.Nickname, expr.Value(base.Nickname)),
		rowset.Map(reports.UserSummaries.Status, expr.Value(base.Status)),
	)
	names := rowset.AsRecord[reportAlias](rowset.RecordOf(summary), "summary")
	nf := rowset.Scope(names)
	if emails, err := rowset.ReadScalar(rowset.OrderScalar(rowset.WhereScalar(rowset.SelectScalar(names, expr.Row(rowset.At(nf, reports.UserSummaries.Email)).Value()), rowset.At(nf, reports.UserSummaries.Status).Eq(models.StatusActive)), rowset.At(nf, reports.UserSummaries.Email).Asc())).All(t.Context(), tx); err != nil || len(emails) != 3 {
		t.Error("derived enum or renamed column failed", err)
	}
	return nil
}

func checkSubqueryValues(t *testing.T, tx *database.Tx, users []models.User) error {
	u, o := models.Users, models.Orders
	buyers := rowset.WhereScalar(rowset.SelectScalar(rowset.From(models.Orders.Query()), expr.Row(expr.Value(o.BuyerID)).Value()), expr.ModelPredicate(o.TotalCents.Gte(2)))
	matched, err := expr.Where(models.Users.Query(), rowset.InQuery(expr.Value(u.ID), buyers)).OrderBy(u.Email.Asc()).All(t.Context(), tx)
	if err != nil {
		return err
	}
	if len(matched) != 2 || matched[0].ID != users[0].ID || matched[1].ID != users[1].ID {
		t.Error("typed membership results changed")
	}
	// SQL NULL remains unknown under NOT IN; explicitly remove it when desired.
	introducers := rowset.SelectScalar(rowset.From(models.Users.Query()), expr.Row(expr.Value(u.IntroducerID)).Value())
	for _, test := range []struct {
		predicate expr.Predicate[models.User]
		want      int64
	}{
		{rowset.InNullableQuery(expr.Value(u.ID), introducers), 1},
		{expr.Not(rowset.InNullableQuery(expr.Value(u.ID), introducers)), 0},
		{expr.Not(rowset.InNullableQuery(expr.Value(u.ID), rowset.WhereScalar(introducers, expr.ModelPredicate(u.IntroducerID.IsNotNull())))), 2},
		{rowset.InQuery(expr.Value(u.ID), rowset.LimitScalar(buyers, 0)), 0},
		{expr.Not(rowset.InQuery(expr.Value(u.ID), rowset.LimitScalar(buyers, 0))), 3},
		{rowset.ExistsQuery(rowset.From(models.Users.Query()), buyers), 3},
		{rowset.ExistsQuery(rowset.From(models.Users.Query()), rowset.LimitScalar(buyers, 0)), 0},
		{expr.Not(rowset.ExistsQuery(rowset.From(models.Users.Query()), buyers)), 0},
	} {
		if n, err := expr.Where(models.Users.Query(), test.predicate).Count(t.Context(), tx); err != nil || n != test.want {
			t.Error("subquery three-valued predicate mismatch", n, test.want, err)
		}
	}
	empty := models.Orders.Query().Where(o.TotalCents.Lt(0))
	count := rowset.SelectScalar(rowset.From(empty), expr.Count[models.Order]().Value())
	if n, err := expr.Where(models.Users.Query(), rowset.ExistsQuery(rowset.From(models.Users.Query()), count)).Count(t.Context(), tx); err != nil || n != 3 {
		t.Error("EXISTS lost scalar aggregate's empty-input row", err)
	}
	if n, err := expr.Where(models.Users.Query(), rowset.ExistsQuery(rowset.From(models.Users.Query()), rowset.HavingScalar(count, expr.Count[models.Order]().Gt(0)))).Count(t.Context(), tx); err != nil || n != 0 {
		t.Error("EXISTS lost HAVING", err)
	}
	last := rowset.LimitScalar(rowset.OrderScalar(buyers, expr.Value(o.TotalCents).Desc()), 1)
	nicknames := rowset.SelectScalar(rowset.From(models.Users.Query().Where(u.ID.Eq(users[1].ID))), expr.Row(expr.Value(u.Nickname)).Value())
	report := rowset.Select(rowset.From(models.Users.Query()), reports.ScalarReports.Row,
		rowset.Map(reports.ScalarReports.ID, expr.Value(u.ID)),
		rowset.MapSelected(reports.ScalarReports.BuyerID, rowset.ScalarQuery(rowset.From(models.Users.Query()), last)),
		rowset.MapSelected(reports.ScalarReports.Nickname, rowset.ScalarNullableQuery(rowset.From(models.Users.Query()), nicknames)),
		rowset.MapSelected(reports.ScalarReports.Orders, rowset.ScalarQuery(rowset.From(models.Users.Query()), count)),
	).OrderBy(expr.Value(u.Email).Asc())
	scalars, err := report.All(t.Context(), tx)
	if err != nil {
		return err
	}
	if len(scalars) != 3 {
		t.Fatal("scalar expressions changed outer cardinality")
	}
	for _, row := range scalars {
		if buyer, ok := row.BuyerID.Get(); !ok || buyer != users[1].ID {
			t.Error("scalar ID codec changed")
		}
		if nickname, ok := row.Nickname.Get(); !ok || nickname != "Bee" {
			t.Error("nullable scalar codec changed")
		}
		if n, ok := row.Orders.Get(); !ok || n != 0 {
			t.Error("scalar aggregate zero became NULL")
		}
	}
	emptyEmails := rowset.LimitScalar(rowset.SelectScalar(rowset.From(models.Users.Query().Where(u.ID.Eq(users[0].ID))), expr.Row(expr.Value(u.Email)).Value()), 0)
	result, err := rowset.ReadScalar(rowset.SelectScalar(rowset.From(models.Users.Query()), rowset.ScalarQuery(rowset.From(models.Users.Query()), emptyEmails))).All(t.Context(), tx)
	if err != nil || len(result) != 3 || !result[0].IsNull() {
		t.Error("empty scalar did not become NULL", err)
	}
	nullNickname := rowset.SelectScalar(rowset.From(models.Users.Query().Where(u.ID.Eq(users[0].ID))), expr.Row(expr.Value(u.Nickname)).Value())
	for _, nullableInput := range []rowset.Scalar[models.User, value.Nullable[string]]{nullNickname, rowset.LimitScalar(nicknames, 0)} {
		result, err := rowset.ReadScalar(rowset.SelectScalar(rowset.From(models.Users.Query()), rowset.ScalarNullableQuery(rowset.From(models.Users.Query()), nullableInput))).All(t.Context(), tx)
		if err != nil || len(result) != 3 || !result[0].IsNull() {
			t.Error("nullable scalar did not preserve NULL/empty input", err)
		}
	}
	input := rowset.SelectScalar(rowset.From(models.Users.Query()), expr.Row(expr.Value(u.ID)).Value())
	err = tx.Savepoint(t.Context(), func(child *database.Tx) error {
		rows, err := rowset.ReadScalar(rowset.SelectScalar(rowset.From(models.Users.Query()), rowset.ScalarQuery(rowset.From(models.Users.Query()), input))).All(t.Context(), child)
		var detail *database.Error
		if rows != nil || !errors.As(err, &detail) || detail.SQLState() != "21000" {
			t.Error("multi-row scalar did not retain database cardinality error", err)
		}
		return err
	})
	if err == nil {
		t.Error("multi-row scalar unexpectedly succeeded")
	}
	// Collection failures discard partial values; callbacks and cancellation release rows.
	stop := errors.New("stop value stream")
	if err := rowset.EachRead(t.Context(), tx, rowset.ReadScalar(buyers), func(model.ID[models.User]) error { return stop }); !errors.Is(err, stop) {
		t.Error("value callback error lost", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if rows, err := rowset.ReadScalar(buyers).All(ctx, queryfixture.NoQueries(t)); rows != nil || !errors.Is(err, context.Canceled) {
		t.Error("canceled subquery reached executor", err)
	}
	if _, err := tx.Exec(t.Context(), `UPDATE users SET status='invalid' WHERE email_address=$1`, users[2].Email); err != nil {
		return err
	}
	statuses := rowset.SelectScalar(rowset.From(models.Users.Query().OrderBy(u.Email.Asc())), expr.Row(expr.Value(u.Status)).Value())
	if values, err := rowset.ReadScalar(statuses).All(t.Context(), tx); values != nil || !errors.Is(err, fault.Invalid) {
		t.Error("malformed value exposed partial results", err)
	}
	if _, err := tx.Exec(t.Context(), `UPDATE users SET status='active' WHERE email_address=$1`, users[2].Email); err != nil {
		return err
	}
	if n, err := rowset.ReadScalar(buyers).Count(t.Context(), tx); err != nil || n != 2 {
		t.Error("subquery failure left rows or transaction unusable", err)
	}
	s, err := rowset.CompileRead(rowset.Read(report))
	if err != nil || strings.Count(s.SQL(), "SELECT") != 4 {
		t.Error("scalar report did not use one nested SQL statement", err)
	}
	return nil
}
