package invalid

import (
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
)

type User struct{}
type Order struct{}
type UserPath struct{ ID model.ID[User] }

func invalid(route foundryhttp.Route[UserPath], id model.ID[Order]) {
	_, _ = route.URL(UserPath{ID: id})
}
