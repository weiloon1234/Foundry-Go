package models

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/model"
)

// Order is a persisted customer order.
type Order struct {
	ID         model.ID[Order]
	BuyerID    model.ID[User]
	TotalCents int64
	Buyer      relation.One[User]
}

// OrderTable binds Order's persisted fields.
type OrderTable struct {
	orm.Table[Order, model.ID[Order]]
	ID         orm.Key[Order, model.ID[Order]]
	BuyerID    orm.Column[Order, model.ID[User]]
	TotalCents orm.Number[Order, int64]
	Buyer      orm.One[Order, User]
}

// Orders is the immutable order declaration.
var Orders = orm.Define("orders", func(b orm.Builder[Order], o *Order) OrderTable {
	table := orm.TableOf(b, &o.ID)
	return OrderTable{Table: table, ID: table.Key(), BuyerID: orm.IDOf(b, &o.BuyerID), TotalCents: orm.NumberOf(b, &o.TotalCents), Buyer: orm.OneOf(b, &o.Buyer)}
})
