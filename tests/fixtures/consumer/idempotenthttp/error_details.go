package idempotenthttp

import (
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
)

//foundry:dto
type OrderExistsDetails struct {
	ExistingOrderID model.ID[Order] `json:"existing_order_id"`
	CurrentName     string          `json:"current_name"`
}

var OrderExists = http.DefineErrorDetails("order_exists", 409, "The order already exists.", OrderExistsDetailsJSON())
