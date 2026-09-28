package compilefail

import (
	"context"
	"foundry.test/consumer/httpdto"
	"foundry.test/consumer/models"
	"foundry.test/consumer/realtime"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

func invalid(hub *websocket.Hub, id model.ID[models.Order], guard auth.Guard[models.User]) {
	other := websocket.DefineOutgoing(realtime.UserInbox(guard), "updated", httpdto.OrderResponseJSON())
	_, _ = websocket.Publish(context.Background(), hub, realtime.Orders, id, other, httpdto.OrderResponse{})
}
