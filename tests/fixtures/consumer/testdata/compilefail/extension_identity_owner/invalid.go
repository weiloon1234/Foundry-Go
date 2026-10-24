package invalid

import (
	"foundry.test/consumer/articles"
	"foundry.test/consumer/profiles"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var invalid = orm.IdentityOf(profiles.Profiles.Table, articles.Authors.Key())
