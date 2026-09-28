package invalid

import (
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
)

type User struct{}
type Order struct{}
type UserPath struct{ ID model.ID[User] }

var invalid = foundryhttp.Param[UserPath, model.ID[User]]("user", foundryhttp.ModelIDPath[Order](), func(path *UserPath) *model.ID[User] { return &path.ID })
