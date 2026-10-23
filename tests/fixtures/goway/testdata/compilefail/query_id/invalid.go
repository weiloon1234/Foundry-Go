package query_id

import (
	m "foundry.test/goway/models"
	model "github.com/weiloon1234/Foundry-Go/model"
)

var _, _ = m.Users.Query().Where(m.Users.ID.Eq(model.ID[m.Order]{})).Require(nil, nil)
