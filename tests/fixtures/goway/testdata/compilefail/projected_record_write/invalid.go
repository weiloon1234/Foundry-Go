package invalid

import (
	m "foundry.test/goway/models"
	r "foundry.test/goway/reporting"
)

var _, _ = m.Users.Insert(nil, nil, r.OrderRow{})
