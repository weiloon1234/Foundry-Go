package distinctqueries_test

import (
	"errors"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"

	"github.com/weiloon1234/Foundry-Go/fault"
	"testing"
)

type recordBuyer struct{}
type recordOrder struct{}

func TestRecordDistinctOnInputOrderingParity(t *testing.T) {
	buyers := rows.As[recordBuyer](models.Users.Query(), "buyer")
	orders := rows.As[recordOrder](models.Orders.Query().Where(models.Orders.TotalCents.Gt(0)), "purchase")
	joined := rows.InnerJoin(buyers, orders, rows.On(expr.At(rows.Scope(buyers), models.Users.ID), expr.At(rows.Scope(orders), models.Orders.BuyerID)))
	left, right := rows.LeftScope(joined, rows.Scope(buyers)), rows.RightScope(joined, rows.Scope(orders))
	id, orderID := expr.At(left, models.Users.ID), expr.At(right, models.Orders.ID)
	record := rows.SelectRecord(joined, left)
	selected := rows.DistinctOnRecord(record, expr.Group(id)).OrderBy(id.Asc(), orderID.Desc()).Limit(2)
	got, err := rows.CompileRead(rows.ReadRecord(selected))
	if err != nil {
		t.Fatal(err)
	}

	assertClauseParity(t, got)
	for _, bad := range []rows.RecordRead[rows.Inner[rows.Alias[recordBuyer, models.User], rows.Alias[recordOrder, models.Order]], models.User]{
		rows.DistinctOnRecord(record),
		rows.DistinctOnRecord(record, expr.Group(id)).OrderBy(orderID.Desc()),
		rows.DistinctOnRecord(record, expr.Group(id), expr.Group(id)),
		rows.DistinctOnRecord(record, expr.GroupKey[rows.Inner[rows.Alias[recordBuyer, models.User], rows.Alias[recordOrder, models.Order]]]{}),
	} {
		if _, err := rows.CompileRead(rows.ReadRecord(bad)); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid record DISTINCT ON accepted", err)
		}
	}
	if _, err := rows.CompileRead(rows.ReadRecord(record)); err != nil {
		t.Fatal("clause mutated source", err)
	}
}
