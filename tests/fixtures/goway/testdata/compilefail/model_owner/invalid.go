package model_owner

import (
	m "foundry.test/goway/models"
	model "github.com/weiloon1234/Foundry-Go/model"
)

func takes(model.ID[m.User]) {}
func F()                     { takes(model.ID[m.Order]{}) }
