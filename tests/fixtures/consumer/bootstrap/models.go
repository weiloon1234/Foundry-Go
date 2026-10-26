package bootstrap

import "github.com/weiloon1234/Foundry-Go/model"

type Member struct {
	ID   model.ID[Member]
	Name string
}

type Operator struct {
	ID         model.ID[Operator]
	Department string
}
