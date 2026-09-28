package compilefail

import (
	"foundry.test/consumer/httpdto"
	"foundry.test/consumer/models"
	"foundry.test/consumer/realtime"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

func invalid(guard auth.Guard[models.User]) {
	event := websocket.DefineOutgoing(realtime.UserInbox(guard), "updated", httpdto.UserResponseJSON())
	_ = websocket.Register(realtime.Orders, event.Registration())
}
