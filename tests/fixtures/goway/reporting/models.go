// Package reporting exercises the typed join and projection pipeline.
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

//foundry:model table=report_members var=Members key=ID identity=false
type Member struct {
	ID        model.ID[Member] `foundry:"member=ID"`
	TenantID  int64
	Name      string
	Nickname  value.Nullable[string]
	State     State
	Balance   decimal.Decimal                   `foundry:"role=number"`
	DeletedAt value.Nullable[temporal.DateTime] `foundry:"role=soft-delete"`
	Orders    relation.Many[Order]
}

//foundry:model table=report_orders var=Orders key=ID identity=false
type Order struct {
	ID        model.ID[Order] `foundry:"member=ID"`
	TenantID  int64
	MemberID  model.ID[Member]
	Label     string
	Amount    decimal.Decimal                   `foundry:"role=number"`
	DeletedAt value.Nullable[temporal.DateTime] `foundry:"role=soft-delete"`
	Member    relation.One[Member]
}

var OrderRelations = struct{ Member orm.BelongsTo[Order, Member] }{orm.BelongsToOf(Orders.Member, Orders.MemberID, Members.ID)}
