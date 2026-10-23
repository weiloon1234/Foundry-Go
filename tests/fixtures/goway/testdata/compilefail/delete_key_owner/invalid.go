package invalid

import (
	m "foundry.test/goway/models"
)

var _, _ = m.Users.Delete(nil, nil, m.Order{}.ID)
