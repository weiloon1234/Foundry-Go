package profiles

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

// ProfileIdentity preserves the existing persisted identity. Example: ProfileIdentity.ReferenceOf(profile).
// doc-example:begin model-extensions-profile-identity
var ProfileIdentity = orm.IdentityOf(Profiles.Table, Profiles.Key())

// doc-example:end model-extensions-profile-identity

// Tables publishes the profile declaration to application assembly. Example: orm.Check(Tables()...).
func Tables() []orm.CheckedTable { return []orm.CheckedTable{Profiles} }
