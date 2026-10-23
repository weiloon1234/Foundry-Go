// Package sensitive is the independent consumer for stored secrets and hashes.
package sensitive

import (
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/database/encrypted"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Settings struct {
	Region string `json:"region"`
}
type Vault struct {
	ID       model.ID[Vault]
	Token    encrypted.Text
	Backup   value.Nullable[encrypted.Text]
	Settings encrypted.JSON[Settings]
	Optional value.Nullable[encrypted.JSON[Settings]]
}
type VaultTable struct {
	orm.Table[Vault, model.ID[Vault]]
	Token    orm.Encrypted[Vault, encrypted.Text]
	Backup   orm.NullableEncrypted[Vault, encrypted.Text]
	Settings orm.Encrypted[Vault, encrypted.JSON[Settings]]
	Optional orm.NullableEncrypted[Vault, encrypted.JSON[Settings]]
}

var Vaults = orm.Define("sensitive_vaults", func(b orm.Builder[Vault], m *Vault) VaultTable {
	return VaultTable{orm.TableOf(b, &m.ID), orm.EncryptedTextOf(b, &m.Token), orm.NullableEncryptedTextOf(b, &m.Backup), orm.EncryptedJSONOf(b, &m.Settings), orm.NullableEncryptedJSONOf(b, &m.Optional)}
})

type Account struct {
	ID     model.ID[Account]
	Digest password.Hash
	Backup value.Nullable[password.Hash]
}
type AccountTable struct {
	orm.Table[Account, model.ID[Account]]
	Digest orm.Column[Account, password.Hash]
	Backup orm.NullableColumn[Account, password.Hash]
}

var Accounts = orm.Define("sensitive_accounts", func(b orm.Builder[Account], m *Account) AccountTable {
	return AccountTable{orm.TableOf(b, &m.ID), orm.PasswordOf(b, &m.Digest), orm.NullablePasswordOf(b, &m.Backup)}
})
