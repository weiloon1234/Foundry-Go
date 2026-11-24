package multifactor

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

// AccountIdentity preserves reference wire identity and key ownership. Example: AccountIdentity.ReferenceOf(account).
var AccountIdentity = orm.IdentityOf(AccountRecords.Table, AccountRecords.Key())

// Tables publishes the account declaration. Example: orm.Check(Tables()...).
func Tables() []orm.CheckedTable { return []orm.CheckedTable{AccountRecords} }

// FoundryIdentity implements the concrete authentication subject contract.
// Example: identity, err := account.FoundryIdentity().
func (account Account) FoundryIdentity() (model.Identity, error) {
	return AccountIdentity.ReferenceOf(account).Identity()
}
