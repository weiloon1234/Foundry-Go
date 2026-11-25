package passwords

import (
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

// doc-example:begin password-hashing-account-table
// Digest deliberately avoids a conventional sensitive field name: the
// password.Hash type, not the name, owns redaction.
//
//foundry:model table=password_accounts var=AccountRecords key=ID identity=false
type Account struct {
	// Table owns the concrete account key. Example: orm.Eq(AccountRecords.Key(), id).
	ID model.ID[Account]
	// Email binds the stored field. Example: orm.Set(AccountRecords.Email, "member@example.test").
	Email string
	// Sensitive stored password hash: typed persistence uses password.Codec; ordinary formatting and JSON are redacted. Automatic audit values are redacted and cursor/identity keys are rejected. Compare-and-swap with the stored Hash; verify plaintext with password.Hasher.Check rather than SQL equality.
	//
	// Digest binds the stored field. Sensitive stored hash; plaintext verification uses password.Hasher. Example: orm.Set(AccountRecords.Digest, hash).
	Digest password.Hash
	// Sensitive stored password hash: typed persistence uses password.Codec; ordinary formatting and JSON are redacted. Automatic audit values are redacted and cursor/identity keys are rejected. Compare-and-swap with the stored Hash; verify plaintext with password.Hasher.Check rather than SQL equality.
	//
	// Backup binds the stored field. Sensitive stored hash; plaintext verification uses password.Hasher. Example: orm.Set(AccountRecords.Backup, hash).
	Backup value.Nullable[password.Hash]
	// Enabled binds the stored field. Example: orm.Set(AccountRecords.Enabled, true).
	Enabled bool `foundry:"role=column"`
}

// doc-example:end password-hashing-account-table

// AccountIdentity preserves reference wire identity and key ownership. Example: AccountIdentity.ReferenceOf(account).
var AccountIdentity = orm.IdentityOf(AccountRecords.Table, AccountRecords.Key())

// Tables publishes the account declaration. Example: orm.Check(Tables()...).
func Tables() []orm.CheckedTable { return []orm.CheckedTable{AccountRecords} }

// FoundryIdentity implements the concrete authentication subject contract.
// Example: identity, err := account.FoundryIdentity().
func (account Account) FoundryIdentity() (model.Identity, error) {
	return AccountIdentity.ReferenceOf(account).Identity()
}
