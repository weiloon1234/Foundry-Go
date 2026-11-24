// Package auditqueries is an independent consumer of generated model auditing.
package auditqueries

import (
	"strings"

	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Preferences struct {
	Theme    string `json:"theme"`
	APIToken string `json:"apiToken"`
}

//foundry:model table=audited_accounts var=Accounts key=ID identity=false hooks=ormAccountHooks
type Account struct {
	ID model.ID[Account]
	// Email stores the normalized address used by audit snapshots.
	// MutateEmail normalizes input; AccessEmail returns display text without changing storage.
	Email        string `foundry:"input=string"`
	PasswordHash string
	Note         value.Nullable[string]
	Balance      decimal.Decimal `foundry:"role=number"`
	Preferences  value.JSON[Preferences]
	CreatedAt    temporal.DateTime                 `foundry:"role=created-at"`
	UpdatedAt    temporal.DateTime                 `foundry:"role=updated-at"`
	DeletedAt    value.Nullable[temporal.DateTime] `foundry:"role=soft-delete"`
}

func (Account) MutateEmail(input string) (string, error) {
	return strings.ToLower(strings.TrimSpace(input)), nil
}
func (a Account) AccessEmail() (string, error) { return "display:" + a.Email, nil }

// This acceptance trap ensures auditing never serializes a model as a DTO.
func (Account) MarshalJSON() ([]byte, error) { panic("model presentation must not own audit storage") }

type LabelCode string

//foundry:model table=audited_labels var=Labels key=Code identity=false
type Label struct {
	Code LabelCode
	Name string
}

// Labels demonstrate that stored natural keys, not presentation methods, own
// audit subjects. AuditHistory must retain the concrete LabelCode type.
func (LabelCode) MarshalJSON() ([]byte, error) { panic("stored audit key must use its database codec") }
