package passwords

import (
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

// AccountTable publishes typed stored fields for the handwritten declaration.
// Example: AccountRecords.Query().
type AccountTable struct {
	// Table owns the concrete account key. Example: orm.KeyEq(AccountRecords.Key(), id).
	orm.Table[Account, model.ID[Account]]
	// Email binds the stored field. Example: orm.TextSet(AccountRecords.Email, "member@example.test").
	Email orm.Text[Account, string]
	// Digest binds the stored field. Sensitive stored hash; plaintext verification uses password.Hasher. Example: orm.ColumnSet(AccountRecords.Digest, hash).
	Digest orm.Column[Account, password.Hash]
	// Backup binds the stored field. Sensitive stored hash; plaintext verification uses password.Hasher. Example: orm.NullableColumnSet(AccountRecords.Backup, hash).
	Backup orm.NullableColumn[Account, password.Hash]
	// Enabled binds the stored field. Example: orm.ColumnSet(AccountRecords.Enabled, true).
	Enabled orm.Column[Account, bool]
}

// AccountRecords preserves the persisted account schema. Example: AccountRecords.Query().
var AccountRecords = orm.Define("password_accounts", func(b orm.Builder[Account], m *Account) AccountTable {
	return AccountTable{orm.TableOf(b, &m.ID), orm.TextOf(b, &m.Email), orm.PasswordOf(b, &m.Digest), orm.NullablePasswordOf(b, &m.Backup), orm.ColumnOf(b, &m.Enabled)}
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
