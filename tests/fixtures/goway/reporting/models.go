// Package reporting exercises the hand-declared join and projection pipeline.
package reporting

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:enum
type State string

const (
	Active   State = "active"
	Disabled State = "disabled"
)

type Member struct {
	ID        model.ID[Member]
	TenantID  int64
	Name      string
	Nickname  value.Nullable[string]
	State     State
	Balance   decimal.Decimal
	DeletedAt value.Nullable[temporal.DateTime]
	Orders    relation.Many[Order]
}
type Order struct {
	ID        model.ID[Order]
	TenantID  int64
	MemberID  model.ID[Member]
	Label     string
	Amount    decimal.Decimal
	DeletedAt value.Nullable[temporal.DateTime]
	Member    relation.One[Member]
}
type MemberTable struct {
	orm.Table[Member, model.ID[Member]]
	ID        orm.Key[Member, model.ID[Member]]
	TenantID  orm.Number[Member, int64]
	Name      orm.Text[Member, string]
	Nickname  orm.NullableText[Member, string]
	State     orm.Enum[Member, State]
	Balance   orm.Number[Member, decimal.Decimal]
	DeletedAt orm.SoftDelete[Member, temporal.DateTime]
	Orders    orm.Many[Member, Order]
}

var Members = orm.Define("report_members", func(b orm.Builder[Member], m *Member) MemberTable {
	tbl := orm.TableOf(b, &m.ID)
	return MemberTable{Table: tbl, ID: tbl.Key(), TenantID: orm.NumberOf(b, &m.TenantID), Name: orm.TextOf(b, &m.Name), Nickname: orm.NullableTextOf(b, &m.Nickname), State: orm.EnumOf(b, &m.State), Balance: orm.NumberOf(b, &m.Balance), DeletedAt: orm.SoftDeleteOf(b, &m.DeletedAt), Orders: orm.ManyOf(b, &m.Orders)}
})

type OrderTable struct {
	orm.Table[Order, model.ID[Order]]
	ID        orm.Key[Order, model.ID[Order]]
	TenantID  orm.Number[Order, int64]
	MemberID  orm.Column[Order, model.ID[Member]]
	Label     orm.Text[Order, string]
	Amount    orm.Number[Order, decimal.Decimal]
	DeletedAt orm.SoftDelete[Order, temporal.DateTime]
	Member    orm.One[Order, Member]
}

var Orders = orm.Define("report_orders", func(b orm.Builder[Order], o *Order) OrderTable {
	tbl := orm.TableOf(b, &o.ID)
	return OrderTable{Table: tbl, ID: tbl.Key(), TenantID: orm.NumberOf(b, &o.TenantID), MemberID: orm.IDOf(b, &o.MemberID), Label: orm.TextOf(b, &o.Label), Amount: orm.NumberOf(b, &o.Amount), DeletedAt: orm.SoftDeleteOf(b, &o.DeletedAt), Member: orm.OneOf(b, &o.Member)}
})

var OrderRelations = struct{ Member orm.BelongsTo[Order, Member] }{orm.BelongsToOf(Orders.Member, Orders.MemberID, Members.ID)}
