package bootstrap

import "github.com/weiloon1234/Foundry-Go/model"

//foundry:model table=bootstrap_members
type Member struct {
	ID   model.ID[Member]
	Name string
}

//foundry:model table=bootstrap_operators
type Operator struct {
	ID         model.ID[Operator]
	Department string
}
