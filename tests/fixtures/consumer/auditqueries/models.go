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

//foundry:model table=audited_accounts hooks=accountHooks
type Account struct {
	ID model.ID[Account]
	// Foundry field behavior (generated): Account.Email retains stored audited_accounts.email. Custom getter: [Account.AccessEmail]; choose the stored field or getter result explicitly when mapping a DTO.
	// Foundry field behavior (generated): Account.Email retains stored audited_accounts.email. Custom setter: [Account.MutateEmail] transforms assigned values during persistence through [AccountDraft.SetEmail]. Direct field assignment and draft construction do not invoke it.
	Email        string
	PasswordHash string
	Note         value.Nullable[string]
	Balance      decimal.Decimal
	Preferences  value.JSON[Preferences]
	// Foundry field behavior (generated): Managed creation timestamp: persistence supplies the owning application clock when omitted, after before-write hooks and before field mutators. An explicit input is preserved.
	CreatedAt temporal.DateTime
	// Foundry field behavior (generated): Managed update timestamp: persistence replaces assigned or omitted input with the owning application clock after before-write hooks and before field mutators. Conflict updates copy its normalized proposed value.
	UpdatedAt temporal.DateTime
	// Foundry field behavior (generated): Managed soft-delete timestamp: Delete sets this stored field and Restore clears it. Ordinary queries exclude deleted models; WithTrashed and OnlyTrashed select visibility explicitly. Assigning this field directly through a draft uses ordinary create/update hooks rather than deletion/restoration events.
	DeletedAt value.Nullable[temporal.DateTime]
}

func (Account) MutateEmail(input string) (string, error) {
	return strings.ToLower(strings.TrimSpace(input)), nil
}
func (a Account) AccessEmail() (string, error) { return "display:" + a.Email, nil }

// This acceptance trap ensures auditing never serializes a model as a DTO.
func (Account) MarshalJSON() ([]byte, error) { panic("model presentation must not own audit storage") }

type LabelCode string

//foundry:model table=audited_labels primary=Code
type Label struct {
	Code LabelCode
	Name string
}

// Labels demonstrate that stored natural keys, not presentation methods, own
// audit subjects. AuditHistory must retain the concrete LabelCode type.
func (LabelCode) MarshalJSON() ([]byte, error) { panic("stored audit key must use its database codec") }
