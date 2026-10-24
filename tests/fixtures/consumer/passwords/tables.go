package passwords

import (
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

// AccountTable publishes typed stored fields alongside the retained generated API.
// Example: AccountRecords.Query().
type AccountTable struct {
	// Table owns the concrete account key. Example: AccountRecords.Key().Eq(id).
	orm.Table[Account, model.ID[Account]]
	// Email binds the stored field. Example: AccountRecords.Email.Set("member@example.test").
	Email orm.Text[Account, string]
	// Digest binds the stored field. Sensitive stored hash; plaintext verification uses password.Hasher. Example: AccountRecords.Digest.Set(hash).
	Digest orm.Column[Account, password.Hash]
	// Backup binds the stored field. Sensitive stored hash; plaintext verification uses password.Hasher. Example: AccountRecords.Backup.Set(hash).
	Backup orm.NullableColumn[Account, password.Hash]
	// Enabled binds the stored field. Example: AccountRecords.Enabled.Set(true).
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
