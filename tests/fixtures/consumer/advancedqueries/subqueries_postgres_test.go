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
	"github.com/weiloon1234/Foundry-Go/database/query"
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
	o := models.OrderFields()
	totals := reports.ProjectBuyerTotals(models.QueryOrders()).SelectBuyerID(o.BuyerID.Value()).
		SelectTotal(o.TotalCents.Sum().Value()).SelectOrders(query.Count[models.Order]().Value()).
		SelectAverage(o.TotalCents.Avg().Value()).Query().GroupBy(o.BuyerID.Group())
	// Grouping, HAVING and a selected window remain inside the derived source.
	source := query.As[totalsAlias](totals.Having(query.Count[models.Order]().Gte(2)).OrderBy(query.Count[models.Order]().Desc()).Limit(1), "totals")
	fields := reports.BuyerTotalsFieldsAt(source.Scope())
	values := query.SelectValue(source, fields.Total.Value()).Where(fields.Total.Gt(decimal.FromInt64(2)))
	amounts, err := values.All(t.Context(), tx)
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
	u := query.As[buyerAlias](models.QueryUsers(), "buyer")
	joined := query.LeftJoin(u, source, query.On(models.UserFieldsAt(u.Scope()).ID, fields.BuyerID))
	buyer := models.UserFieldsAt(query.LeftScope(joined, u.Scope()))
	stats := reports.BuyerTotalsNullableFieldsAt(query.NullableRightScope(joined, source.Scope()))
	report := reports.ProjectUserOrderStats(joined).SelectID(buyer.ID.Value()).SelectEmail(buyer.Email.Value()).
		SelectTotal(stats.Total.Value()).SelectOrders(stats.Orders.Value()).Query().OrderBy(buyer.Email.Asc())
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
	derived := query.As[reportAlias](report, "report")
	df := reports.UserOrderStatsFieldsAt(derived.Scope())
	again := query.InnerJoin(u, derived, query.On(models.UserFieldsAt(u.Scope()).ID, df.ID))
	joinedFields := reports.UserOrderStatsFieldsAt(query.RightScope(again, derived.Scope()))
	if emails, err := query.SelectValue(again, joinedFields.Email.Value()).OrderBy(joinedFields.Email.Asc()).All(t.Context(), tx); err != nil || len(emails) != 3 || emails[0] != users[0].Email {
		t.Error("declared joined result could not be joined again", err)
	}
	// Derived enum fields retain enum operators and decoders, including output aliases.
	base := models.UserFields()
	summary := reports.ProjectUserSummary(models.QueryUsers()).SelectID(base.ID.Value()).SelectEmail(base.Email.Value()).SelectNickname(base.Nickname.Value()).SelectStatus(base.Status.Value()).Query()
	names := query.As[reportAlias](summary, "summary")
	nf := reports.UserSummaryFieldsAt(names.Scope())
	if emails, err := query.SelectValue(names, nf.Email.Value()).Where(nf.Status.Eq(models.StatusActive)).OrderBy(nf.Email.Asc()).All(t.Context(), tx); err != nil || len(emails) != 3 {
		t.Error("derived enum or renamed column failed", err)
	}
	return nil
}

func checkSubqueryValues(t *testing.T, tx *database.Tx, users []models.User) error {
	u, o := models.UserFields(), models.OrderFields()
	buyers := query.SelectValue(models.QueryOrders(), o.BuyerID.Value()).Where(o.TotalCents.Gte(2))
	matched, err := models.QueryUsers().Where(u.ID.InQuery(buyers)).OrderBy(u.Email.Asc()).All(t.Context(), tx)
	if err != nil {
		return err
	}
	if len(matched) != 2 || matched[0].ID != users[0].ID || matched[1].ID != users[1].ID {
		t.Error("typed membership results changed")
	}
	// SQL NULL remains unknown under NOT IN; explicitly remove it when desired.
	introducers := query.SelectValue(models.QueryUsers(), u.IntroducerID.Value())
	for _, test := range []struct {
		predicate query.Predicate[models.User]
		want      int64
	}{
		{u.ID.InNullableQuery(introducers), 1},
		{u.ID.InNullableQuery(introducers).Not(), 0},
		{u.ID.InNullableQuery(introducers.Where(u.IntroducerID.IsNotNull())).Not(), 2},
		{u.ID.InQuery(buyers.Limit(0)), 0},
		{u.ID.InQuery(buyers.Limit(0)).Not(), 3},
		{query.ExistsQuery(models.QueryUsers(), buyers), 3},
		{query.ExistsQuery(models.QueryUsers(), buyers.Limit(0)), 0},
		{query.ExistsQuery(models.QueryUsers(), buyers).Not(), 0},
	} {
		if n, err := models.QueryUsers().Where(test.predicate).Count(t.Context(), tx); err != nil || n != test.want {
			t.Error("subquery three-valued predicate mismatch", n, test.want, err)
		}
	}
	empty := models.QueryOrders().Where(o.TotalCents.Lt(0))
	count := query.SelectValue(empty, query.Count[models.Order]().Value())
	if n, err := models.QueryUsers().Where(query.ExistsQuery(models.QueryUsers(), count)).Count(t.Context(), tx); err != nil || n != 3 {
		t.Error("EXISTS lost scalar aggregate's empty-input row", err)
	}
	if n, err := models.QueryUsers().Where(query.ExistsQuery(models.QueryUsers(), count.Having(query.Count[models.Order]().Gt(0)))).Count(t.Context(), tx); err != nil || n != 0 {
		t.Error("EXISTS lost HAVING", err)
	}
	last := buyers.OrderBy(o.TotalCents.Desc()).Limit(1)
	nicknames := query.SelectValue(models.QueryUsers().Where(u.ID.Eq(users[1].ID)), u.Nickname.Value())
	report := reports.ProjectScalarReport(models.QueryUsers()).SelectID(u.ID.Value()).
		SelectBuyerID(query.ScalarQuery(models.QueryUsers(), last)).
		SelectNickname(query.ScalarNullableQuery(models.QueryUsers(), nicknames)).
		SelectOrders(query.ScalarQuery(models.QueryUsers(), count)).Query().OrderBy(u.Email.Asc())
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
	emptyEmails := query.SelectValue(models.QueryUsers().Where(u.ID.Eq(users[0].ID)), u.Email.Value()).Limit(0)
	result, err := query.SelectValue(models.QueryUsers(), query.ScalarQuery(models.QueryUsers(), emptyEmails)).All(t.Context(), tx)
	if err != nil || len(result) != 3 || !result[0].IsNull() {
		t.Error("empty scalar did not become NULL", err)
	}
	nullNickname := query.SelectValue(models.QueryUsers().Where(u.ID.Eq(users[0].ID)), u.Nickname.Value())
	for _, nullableInput := range []query.ValueQuery[models.User, value.Nullable[string]]{nullNickname, nicknames.Limit(0)} {
		result, err := query.SelectValue(models.QueryUsers(), query.ScalarNullableQuery(models.QueryUsers(), nullableInput)).All(t.Context(), tx)
		if err != nil || len(result) != 3 || !result[0].IsNull() {
			t.Error("nullable scalar did not preserve NULL/empty input", err)
		}
	}
	input := query.SelectValue(models.QueryUsers(), u.ID.Value())
	err = tx.Savepoint(t.Context(), func(child *database.Tx) error {
		rows, err := query.SelectValue(models.QueryUsers(), query.ScalarQuery(models.QueryUsers(), input)).All(t.Context(), child)
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
	if err := buyers.Each(t.Context(), tx, func(model.ID[models.User]) error { return stop }); !errors.Is(err, stop) {
		t.Error("value callback error lost", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if rows, err := buyers.All(ctx, queryfixture.NoQueries(t)); rows != nil || !errors.Is(err, context.Canceled) {
		t.Error("canceled subquery reached executor", err)
	}
	if _, err := tx.Exec(t.Context(), `UPDATE users SET status='invalid' WHERE email_address=$1`, users[2].Email); err != nil {
		return err
	}
	statuses := query.SelectValue(models.QueryUsers().OrderBy(u.Email.Asc()), u.Status.Value())
	if values, err := statuses.All(t.Context(), tx); values != nil || !errors.Is(err, fault.Invalid) {
		t.Error("malformed value exposed partial results", err)
	}
	if _, err := tx.Exec(t.Context(), `UPDATE users SET status='active' WHERE email_address=$1`, users[2].Email); err != nil {
		return err
	}
	if n, err := buyers.Count(t.Context(), tx); err != nil || n != 2 {
		t.Error("subquery failure left rows or transaction unusable", err)
	}
	s, err := report.Compile()
	if err != nil || strings.Count(s.SQL(), "SELECT") != 4 {
		t.Error("scalar report did not use one nested SQL statement", err)
	}
	return nil
}
