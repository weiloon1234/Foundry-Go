package query_id

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	m "foundry.test/goway/models"
	model "github.com/weiloon1234/Foundry-Go/model"
)

var _, _ = orm.Require(nil, nil, m.Users.Query().Where(orm.KeyEq(m.Users.ID, model.ID[m.Order]{})))
