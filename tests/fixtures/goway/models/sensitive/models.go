// Package sensitive is the independent consumer for stored secrets and hashes.
package sensitive

import (
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/database/encrypted"

	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Settings struct {
	Region string `json:"region"`
}

//foundry:model table=sensitive_vaults var=Vaults key=ID identity=false
type Vault struct {
	ID       model.ID[Vault]
	Token    encrypted.Text
	Backup   value.Nullable[encrypted.Text]
	Settings encrypted.JSON[Settings]
	Optional value.Nullable[encrypted.JSON[Settings]]
}

//foundry:model table=sensitive_accounts var=Accounts key=ID identity=false
type Account struct {
	ID     model.ID[Account]
	Digest password.Hash
	Backup value.Nullable[password.Hash]
}
