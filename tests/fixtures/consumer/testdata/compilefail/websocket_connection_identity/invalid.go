package compilefail

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

func invalid(h *websocket.Hub, id websocket.MessageID) {
	_ = websocket.DisconnectConnection(context.Background(), h, id)
}
