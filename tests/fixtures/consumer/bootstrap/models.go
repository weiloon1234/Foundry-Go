package bootstrap

import "github.com/weiloon1234/Foundry-Go/model"

//foundry:model table=bootstrap_members var=Members key=ID identity=false
type Member struct {
	ID   model.ID[Member] `foundry:"member=ID"`
	Name string
}

//foundry:model table=bootstrap_operators var=Operators key=ID identity=false
type Operator struct {
	ID         model.ID[Operator] `foundry:"member=ID"`
	Department string
}
