package reporting

import (
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

// MemberRow is the migration guide's concrete report projection.
type MemberRow struct {
	ID       model.ID[Member]       `json:"id"`
	Name     string                 `json:"name"`
	Nickname value.Nullable[string] `json:"nickname"`
	State    State                  `json:"state"`
	Balance  decimal.Decimal        `json:"balance"`
}
type MemberRowFields struct {
	rows.Row[MemberRow]
	ID       rows.Field[MemberRow, model.ID[Member]]
	Name     rows.Field[MemberRow, string]
	Nickname rows.Field[MemberRow, value.Nullable[string]]
	State    rows.Field[MemberRow, State]
	Balance  rows.Field[MemberRow, decimal.Decimal]
}

var MemberRows = rows.Define(func(b rows.Builder[MemberRow], r *MemberRow) MemberRowFields {
	return MemberRowFields{b.Row(), rows.IDOf(b, &r.ID), rows.TextOf(b, &r.Name), rows.NullableTextOf(b, &r.Nickname), rows.EnumOf(b, &r.State), rows.NumberOf(b, &r.Balance)}
})
