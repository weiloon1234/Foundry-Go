package consumer

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/model"
)

// doc-example:begin model-id-user
type User struct{ ID model.ID[User] }

// doc-example:end model-id-user

// doc-example:begin allocate-model-id
func NewUser(now time.Time) (User, error) {
	id, err := model.NewIDAt[User](now)
	if err != nil {
		return User{}, err
	}
	return User{ID: id}, nil
}

// doc-example:end allocate-model-id
