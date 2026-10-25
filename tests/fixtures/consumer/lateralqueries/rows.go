package lateralqueries

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type UserOrderColumns struct {
	rows.Row[UserOrder]
	UserID  rows.Field[UserOrder, model.ID[models.User]]
	OrderID rows.Field[UserOrder, value.Nullable[model.ID[models.Order]]]
	Total   rows.Field[UserOrder, value.Nullable[int64]]
}

var UserOrders = rows.Define(func(b rows.Builder[UserOrder], r *UserOrder) UserOrderColumns {
	return UserOrderColumns{b.Row(), rows.IDOf(b, &r.UserID), rows.NullableIDOf(b, &r.OrderID), rows.NullableNumberOf(b, &r.Total)}
})

type RankedOrderColumns struct {
	rows.Row[RankedOrder]
	ID    rows.Field[RankedOrder, model.ID[models.Order]]
	Total rows.Field[RankedOrder, int64]
	Rank  rows.Field[RankedOrder, int64]
}

var RankedOrders = rows.Define(func(b rows.Builder[RankedOrder], r *RankedOrder) RankedOrderColumns {
	return RankedOrderColumns{b.Row(), rows.IDOf(b, &r.ID), rows.NumberOf(b, &r.Total), rows.NumberOf(b, &r.Rank)}
})
