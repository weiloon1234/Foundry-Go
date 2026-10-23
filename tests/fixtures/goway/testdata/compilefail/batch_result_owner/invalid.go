package invalid

import (
	m "foundry.test/goway/models"
)

func bad() { items, _ := m.Users.InsertMany(nil, nil, nil); var _ []m.Order = items }
