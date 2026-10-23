package imported_enum_text_operator

import (
	c "foundry.test/goway/coremodels"
)

var _ = c.Records.Status.Like("active%")
