// Package reporting exercises generated report contracts through public APIs.
package reporting

import (
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

type Operator struct {
	ID        model.ID[Operator]
	TenantID  int64
	Active    bool
	CanView   bool
	CanExport bool
}

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

// Ordinary DTO declarations own JSON and validation; rows.go owns SQL mappings.
//
//foundry:dto
type MemberRow struct {
	ID       model.ID[Member]       `json:"id"`
	Name     string                 `json:"name"`
	Nickname value.Nullable[string] `json:"nickname"`
	State    State                  `json:"state"`
	Balance  decimal.Decimal        `json:"balance"`
}

//foundry:dto
type OrderRow struct {
	ID         model.ID[Order] `json:"id"`
	MemberName string          `json:"memberName"`
	Label      string          `json:"label"`
	Amount     decimal.Decimal `json:"amount"`
}

//foundry:dto
type MemberTotal struct {
	MemberID model.ID[Member]                `json:"memberId"`
	Count    int64                           `json:"count"`
	Total    value.Nullable[decimal.Decimal] `json:"total"`
}
