package invalid

import (
	m "foundry.test/goway/models"
)

var _, _ = m.Users.Update(nil, nil, m.Order{}.ID)
