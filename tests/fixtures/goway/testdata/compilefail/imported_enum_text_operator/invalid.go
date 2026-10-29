package imported_enum_text_operator

import (
	c "foundry.test/goway/coremodels"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.TextLike(c.Records.Status, "active%")
