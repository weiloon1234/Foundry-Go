package invalid

import "github.com/weiloon1234/Foundry-Go/model"

type User struct{}
type Order struct{}

var invalid = model.ID[User](model.ID[Order]{})
