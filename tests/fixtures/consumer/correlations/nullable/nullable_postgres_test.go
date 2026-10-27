package nullable_test

import (
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type sponsorQueryAlias struct{}
type buyerAlias struct{}

func TestPostgresNullableCorrelations(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, _ []models.User) error { return checkNullableCorrelation(t, tx) })
}

func checkNullableCorrelation(t *testing.T, tx *database.Tx) error {
	users := rows.As[buyerAlias](models.Users.Query(), "outer_user")
	sponsors := rows.As[sponsorQueryAlias](models.Users.Query(), "outer_sponsor")
	u, s := rows.Scope(users), rows.Scope(sponsors)
	joined := rows.LeftJoin(users, sponsors, rows.On(expr.At(u, models.Users.IntroducerID), expr.At(s, models.Users.ID)))
	link := rows.Correlate(joined, rows.From(models.Orders.Query()))
	sponsorScope := rows.NullableRightScope(joined, rows.Scope(sponsors))
	sponsor := rows.OuterNullableScope(link, sponsorScope)
	order := rows.InnerScope(link, rows.ModelScope(models.Orders.Query()))
	count := rows.SelectCorrelatedScalar(link, expr.CountFieldAt(order, models.Orders.ID).Value()).Where(expr.Equal(expr.NullableRow(expr.At(order, models.Orders.BuyerID)), expr.AtNullable(sponsor, models.Users.ID)))
	outerUser := rows.LeftScope(joined, rows.Scope(users))
	values, err := rows.ReadScalar(rows.SelectScalar(joined, rows.CorrelatedScalarRowQuery(count).Value())).OrderBy(expr.At(outerUser, models.Users.Email).Asc()).All(t.Context(), tx)
	if err != nil {
		return err
	}
	if len(values) != 3 {
		t.Fatal("outer nullable correlation changed cardinality")
	}
	for i, want := range []int64{0, 2, 0} {
		if n, ok := values[i].Get(); !ok || n != want {
			t.Error("nullable outer key changed correlation", i, n, want)
		}
	}
	// A nullable record can also belong to the inner joined SELECT. Missing
	// sponsors must remain NULL when selecting their email for each order.
	innerJoin := rows.Correlate(rows.From(models.Orders.Query()), joined)
	outerOrder := rows.OuterScope(innerJoin, rows.ModelScope(models.Orders.Query()))
	innerUser := rows.InnerScope(innerJoin, rows.LeftScope(joined, rows.Scope(users)))
	innerSponsor := rows.InnerNullableScope(innerJoin, sponsorScope)
	emails := rows.SelectCorrelatedScalar(innerJoin, expr.Row(expr.AtNullable(innerSponsor, models.Users.Email)).Value()).Where(expr.Equal(expr.At(innerUser, models.Users.ID), expr.At(outerOrder, models.Orders.BuyerID)))
	selected, err := rows.ReadScalar(rows.SelectScalar(rows.From(models.Orders.Query()), rows.CorrelatedScalarNullableRowQuery(emails).Value())).
		OrderBy(expr.Value(models.Orders.TotalCents).Asc()).All(t.Context(), tx)
	if err != nil {
		return err
	}
	if len(selected) != 3 || !selected[0].IsNull() || !selected[1].IsNull() {
		t.Fatal("nullable inner join lost missing sponsors")
	}
	if email, ok := selected[2].Get(); !ok || email != "a@example.test" {
		t.Error("nullable inner join lost its matched sponsor")
	}
	return nil
}
