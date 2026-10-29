package nullable_interval_like

import (
	c "foundry.test/goway/coremodels"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.NullableTextLike(c.Records.MaybePeriod, "%day%")
