package compilefail

import (
	"context"
	"foundry.test/consumer/models"
	"foundry.test/consumer/realtime"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

func invalid(g auth.Guard[models.User]) {
	_ = context.Background()
	out := websocket.RawOutgoing(realtime.UserInbox(g), "raw")
	_ = websocket.RawIncoming(realtime.Orders, "raw").Relay(out)
}
