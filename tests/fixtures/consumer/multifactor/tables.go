package multifactor

import (
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

// AccountTable publishes typed stored fields for the handwritten declaration.
// Example: AccountRecords.Query().
type AccountTable struct {
	// Table owns the concrete account key. Example: AccountRecords.Key().Eq(id).
	orm.Table[Account, model.ID[Account]]
	// Email binds the stored field. Example: AccountRecords.Email.Set("member@example.test").
	Email orm.Text[Account, string]
	// Password binds the stored field. Sensitive stored hash; plaintext verification uses password.Hasher. Example: AccountRecords.Password.Set(hash).
	Password orm.Column[Account, password.Hash]
	// Enabled binds the stored field. Example: AccountRecords.Enabled.Set(true).
	Enabled orm.Column[Account, bool]
	// MFAEnabled binds the stored field. Example: AccountRecords.MFAEnabled.Set(true).
	MFAEnabled orm.Column[Account, bool]
	// RequireMFA binds the stored field. Example: AccountRecords.RequireMFA.Set(true).
	RequireMFA orm.Column[Account, bool]
}

// AccountRecords preserves the persisted account schema. Example: AccountRecords.Query().
var AccountRecords = orm.Define("mfa_accounts", func(b orm.Builder[Account], m *Account) AccountTable {
	return AccountTable{orm.TableOf(b, &m.ID), orm.TextOf(b, &m.Email), orm.PasswordOf(b, &m.Password), orm.ColumnOf(b, &m.Enabled), orm.ColumnOf(b, &m.MFAEnabled), orm.ColumnOf(b, &m.RequireMFA)}
})

// AccountIdentity preserves reference wire identity and key ownership. Example: AccountIdentity.ReferenceOf(account).
var AccountIdentity = orm.IdentityOf(AccountRecords.Table, AccountRecords.Key())

// Tables publishes the account declaration. Example: orm.Check(Tables()...).
func Tables() []orm.CheckedTable { return []orm.CheckedTable{AccountRecords} }

// FoundryIdentity implements the concrete authentication subject contract.
// Example: identity, err := account.FoundryIdentity().
func (account Account) FoundryIdentity() (model.Identity, error) {
	return AccountIdentity.ReferenceOf(account).Identity()
}
