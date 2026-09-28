package invalid

import (
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

type User struct{}
type Order struct{}
type Input struct {
	User value.Optional[model.ID[User]]
}

var field = validation.DefineField("user", func(v Input) value.Optional[model.ID[User]] { return v.User })
var _ = field.Rules(validation.Required[model.ID[Order]]())
