package compilefail

import (
	"context"
	"foundry.test/consumer/httpdto"
	"foundry.test/consumer/models"
	"foundry.test/consumer/realtime"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

var invalid = realtime.Inspect.Handle(func(context.Context, websocket.MessageContext[model.ID[models.Order], models.User], httpdto.OrderResponse) error {
	return nil
})
