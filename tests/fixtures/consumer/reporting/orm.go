package reporting

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

// Handwritten SQL declarations retain primary and soft-delete roles. The
// existing generated model helpers remain only for independent legacy fixtures.
type MemberModelColumns struct {
	orm.Table[Member, model.ID[Member]]
	ID        orm.Primary[Member, model.ID[Member]]
	TenantID  orm.Number[Member, int64]
	Name      orm.Text[Member, string]
	Nickname  orm.NullableText[Member, string]
	State     orm.Enum[Member, State]
	Balance   orm.Number[Member, decimal.Decimal]
	DeletedAt orm.SoftDelete[Member, temporal.DateTime]
	Orders    orm.Many[Member, Order]
}

var MemberModels = orm.Define("report_members", func(b orm.Builder[Member], m *Member) MemberModelColumns {
	tbl := orm.TableOf(b, &m.ID)
	return MemberModelColumns{Table: tbl, ID: orm.PrimaryOf(b, tbl.Key()), TenantID: orm.NumberOf(b, &m.TenantID), Name: orm.TextOf(b, &m.Name), Nickname: orm.NullableTextOf(b, &m.Nickname), State: orm.EnumOf(b, &m.State), Balance: orm.NumberOf(b, &m.Balance), DeletedAt: orm.SoftDeleteOf(b, &m.DeletedAt), Orders: orm.ManyOf(b, &m.Orders)}
})

type OrderModelColumns struct {
	orm.Table[Order, model.ID[Order]]
	ID        orm.Primary[Order, model.ID[Order]]
	TenantID  orm.Number[Order, int64]
	MemberID  orm.Column[Order, model.ID[Member]]
	Label     orm.Text[Order, string]
	Amount    orm.Number[Order, decimal.Decimal]
	DeletedAt orm.SoftDelete[Order, temporal.DateTime]
	Member    orm.One[Order, Member]
}

var OrderModels = orm.Define("report_orders", func(b orm.Builder[Order], o *Order) OrderModelColumns {
	tbl := orm.TableOf(b, &o.ID)
	return OrderModelColumns{Table: tbl, ID: orm.PrimaryOf(b, tbl.Key()), TenantID: orm.NumberOf(b, &o.TenantID), MemberID: orm.IDOf(b, &o.MemberID), Label: orm.TextOf(b, &o.Label), Amount: orm.NumberOf(b, &o.Amount), DeletedAt: orm.SoftDeleteOf(b, &o.DeletedAt), Member: orm.OneOf(b, &o.Member)}
})

var OrderModelRelations = struct{ Member orm.BelongsTo[Order, Member] }{orm.BelongsToOf(OrderModels.Member, orm.ColumnExpr(OrderModels.MemberID), orm.KeyExpr(MemberModels.ID))}

var MemberModelRelations = struct{ Orders orm.HasMany[Member, Order] }{orm.HasManyOf(MemberModels.Orders, orm.KeyExpr(MemberModels.ID), orm.ColumnExpr(OrderModels.MemberID))}

// OperatorModels owns authentication lookups and current report permissions.
var OperatorModels = orm.Define("report_operators", func(b orm.Builder[Operator], o *Operator) OperatorModelColumns {
	return OperatorModelColumns{
		Table:     orm.TableOf(b, &o.ID),
		TenantID:  orm.NumberOf(b, &o.TenantID),
		Active:    orm.ColumnOf(b, &o.Active),
		CanView:   orm.ColumnOf(b, &o.CanView),
		CanExport: orm.ColumnOf(b, &o.CanExport),
	}
})

type OperatorModelColumns struct {
	orm.Table[Operator, model.ID[Operator]]
	TenantID  orm.Number[Operator, int64]
	Active    orm.Column[Operator, bool]
	CanView   orm.Column[Operator, bool]
	CanExport orm.Column[Operator, bool]
}

// OperatorIdentity preserves the stored report_operators identity and key codec.
var OperatorIdentity = orm.IdentityOf(OperatorModels.Table, OperatorModels.Key())
