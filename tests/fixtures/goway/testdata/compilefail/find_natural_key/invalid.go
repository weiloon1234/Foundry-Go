package find_natural_key

import (
	c "foundry.test/goway/coremodels"
	m "foundry.test/goway/models"
)

var _, _ = c.Records.Find(nil, nil, m.StatusActive)
