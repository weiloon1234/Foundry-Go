// Package httpdto proves generated JSON contracts from a separate consumer.
package httpdto

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

// doc-example:begin patch-presence

//foundry:dto
type UpdateUser struct {
	Email    value.Optional[string]                 `json:"email,omitzero"`
	Nickname value.Optional[value.Nullable[string]] `json:"nickname,omitzero"`
	State    value.Optional[models.Status]          `json:"state,omitzero"`
}

// doc-example:end patch-presence

//foundry:dto
type UserResponse struct {
	ID    model.ID[models.User] `json:"id"`
	Email string                `json:"email"`
	State models.Status         `json:"state"`
}

//foundry:dto
type OrderResponse struct {
	ID      model.ID[models.Order] `json:"id"`
	BuyerID model.ID[models.User]  `json:"buyer_id"`
}
