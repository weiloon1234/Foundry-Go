// Package reporting exercises generated report contracts through public APIs.
package reporting

import (
	"github.com/weiloon1234/Foundry-Go/database/query"
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

//foundry:model table=report_operators
type Operator struct {
	ID        model.ID[Operator]
	TenantID  int64
	Active    bool
	CanView   bool
	CanExport bool
}

//foundry:model table=report_members
type Member struct {
	ID       model.ID[Member]
	TenantID int64
	Name     string
	Nickname value.Nullable[string]
	State    State
	Balance  decimal.Decimal
	// Foundry field behavior (generated): Managed soft-delete timestamp: Delete sets this stored field and Restore clears it. Ordinary queries exclude deleted models; WithTrashed and OnlyTrashed select visibility explicitly. Assigning this field directly through a draft uses ordinary create/update hooks rather than deletion/restoration events.
	DeletedAt value.Nullable[temporal.DateTime]
	Orders    relation.Many[Order]
}

//foundry:model table=report_orders
type Order struct {
	ID       model.ID[Order]
	TenantID int64
	MemberID model.ID[Member]
	Label    string
	Amount   decimal.Decimal
	// Foundry field behavior (generated): Managed soft-delete timestamp: Delete sets this stored field and Restore clears it. Ordinary queries exclude deleted models; WithTrashed and OnlyTrashed select visibility explicitly. Assigning this field directly through a draft uses ordinary create/update hooks rather than deletion/restoration events.
	DeletedAt value.Nullable[temporal.DateTime]
	Member    relation.One[Member]
}

func (Member) DefineRelations() MemberRelationSet {
	return MemberRelationSet{Orders: query.HasMany(MemberFields().ID, OrderFields().MemberID)}
}

func (Order) DefineRelations() OrderRelationSet {
	return OrderRelationSet{Member: query.BelongsTo(OrderFields().MemberID, MemberFields().ID)}
}

// One declaration supplies SQL projection mapping, JSON and typed selectors.
//
//foundry:projection dto=true
type MemberRow struct {
	ID       model.ID[Member]       `json:"id"`
	Name     string                 `json:"name"`
	Nickname value.Nullable[string] `json:"nickname"`
	State    State                  `json:"state"`
	Balance  decimal.Decimal        `json:"balance"`
}

//foundry:projection dto=true
type OrderRow struct {
	ID         model.ID[Order] `json:"id"`
	MemberName string          `json:"memberName"`
	Label      string          `json:"label"`
	Amount     decimal.Decimal `json:"amount"`
}

//foundry:projection dto=true
type MemberTotal struct {
	MemberID model.ID[Member]                `json:"memberId"`
	Count    int64                           `json:"count"`
	Total    value.Nullable[decimal.Decimal] `json:"total"`
}
