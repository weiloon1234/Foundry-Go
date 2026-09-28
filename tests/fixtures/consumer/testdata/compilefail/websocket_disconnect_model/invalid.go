package compilefail

import (
	"context"
	"foundry.test/consumer/models"
	"foundry.test/consumer/realtime"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

func invalid(h *websocket.Hub, g auth.Guard[models.User], o models.Order) {
	_ = realtime.Orders
	_ = websocket.DisconnectSubject(context.Background(), h, g, o.FoundryReference())
}
