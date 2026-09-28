package invalid

import "github.com/weiloon1234/Foundry-Go/model"

type User struct{}
type Order struct{}

func takesUser(model.ID[User]) {}
func invalid()                 { takesUser(model.ID[Order]{}) }
