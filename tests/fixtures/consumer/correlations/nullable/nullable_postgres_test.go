package nullable_test

import (
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

type sponsorQueryAlias struct{}
type buyerAlias struct{}

func TestPostgresNullableCorrelations(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, _ []models.User) error { return checkNullableCorrelation(t, tx) })
}

func checkNullableCorrelation(t *testing.T, tx *database.Tx) error {
	users := query.As[buyerAlias](models.QueryUsers(), "outer_user")
	sponsors := query.As[sponsorQueryAlias](models.QueryUsers(), "outer_sponsor")
	u, s := models.UserFieldsAt(users.Scope()), models.UserFieldsAt(sponsors.Scope())
	joined := query.LeftJoin(users, sponsors, query.On(u.IntroducerID, s.ID))
	link := query.Correlate(joined, models.QueryOrders())
	sponsorScope := query.NullableRightScope(joined, sponsors.Scope())
	sponsor := models.UserNullableFieldsAt(query.OuterNullableScope(link, sponsorScope))
	order := models.OrderFieldsAt(query.InnerScope(link, models.QueryOrders().Scope()))
	count := query.SelectCorrelatedValue(link, order.ID.Count().Value()).Where(order.BuyerID.EqColumn(sponsor.ID))
	outerUser := models.UserFieldsAt(query.LeftScope(joined, users.Scope()))
	values, err := query.SelectValue(joined, query.CorrelatedScalarQuery(count)).OrderBy(outerUser.Email.Asc()).All(t.Context(), tx)
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
	innerJoin := query.Correlate(models.QueryOrders(), joined)
	outerOrder := models.OrderFieldsAt(query.OuterScope(innerJoin, models.QueryOrders().Scope()))
	innerUser := models.UserFieldsAt(query.InnerScope(innerJoin, query.LeftScope(joined, users.Scope())))
	innerSponsor := models.UserNullableFieldsAt(query.InnerNullableScope(innerJoin, sponsorScope))
	emails := query.SelectCorrelatedValue(innerJoin, innerSponsor.Email.Value()).Where(innerUser.ID.EqColumn(outerOrder.BuyerID))
	selected, err := query.SelectValue(models.QueryOrders(), query.CorrelatedScalarNullableQuery(emails)).
		OrderBy(models.OrderFields().TotalCents.Asc()).All(t.Context(), tx)
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
