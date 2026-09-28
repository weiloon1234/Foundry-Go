package compilefail

import (
	"context"
	"foundry.test/consumer/httpdto"
	"foundry.test/consumer/models"
	"foundry.test/consumer/realtime"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

func invalid(hub *websocket.Hub, id model.ID[models.Order]) {
	_, _ = websocket.Publish(context.Background(), hub, realtime.Orders, id, realtime.OrderUpdated, httpdto.UserResponse{})
}
