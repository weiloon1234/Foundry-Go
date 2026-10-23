package model_conversion

import (
	m "foundry.test/goway/models"
	model "github.com/weiloon1234/Foundry-Go/model"
)

var _ = model.ID[m.User](model.ID[m.Order]{})
