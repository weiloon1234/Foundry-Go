package reporting

import (
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

// SQL mappings are handwritten; the existing DTO descriptors retain wire ownership.
type MemberRowColumns struct {
	rows.Row[MemberRow]
	ID rows.Field[MemberRow, model.ID[Member]]
	// Display name selected for the member report.
	Name     rows.Field[MemberRow, string]
	Nickname rows.Field[MemberRow, value.Nullable[string]]
	State    rows.Field[MemberRow, State]
	Balance  rows.Field[MemberRow, decimal.Decimal]
}

var MemberRows = rows.Define(func(b rows.Builder[MemberRow], r *MemberRow) MemberRowColumns {
	return MemberRowColumns{b.Row(), rows.IDOf(b, &r.ID), rows.TextOf(b, &r.Name), rows.NullableTextOf(b, &r.Nickname), rows.EnumOf(b, &r.State), rows.NumberOf(b, &r.Balance)}
})

type OrderRowColumns struct {
	rows.Row[OrderRow]
	ID         rows.Field[OrderRow, model.ID[Order]]
	MemberName rows.Field[OrderRow, string]
	Label      rows.Field[OrderRow, string]
	Amount     rows.Field[OrderRow, decimal.Decimal]
}

var OrderRows = rows.Define(func(b rows.Builder[OrderRow], r *OrderRow) OrderRowColumns {
	return OrderRowColumns{b.Row(), rows.IDOf(b, &r.ID), rows.TextOf(b, &r.MemberName), rows.TextOf(b, &r.Label), rows.NumberOf(b, &r.Amount)}
})

type MemberTotalColumns struct {
	rows.Row[MemberTotal]
	MemberID rows.Field[MemberTotal, model.ID[Member]]
	Count    rows.Field[MemberTotal, int64]
	Total    rows.Field[MemberTotal, value.Nullable[decimal.Decimal]]
}

var MemberTotals = rows.Define(func(b rows.Builder[MemberTotal], r *MemberTotal) MemberTotalColumns {
	return MemberTotalColumns{b.Row(), rows.IDOf(b, &r.MemberID), rows.NumberOf(b, &r.Count), rows.NullableNumberOf(b, &r.Total)}
})
