package invalid

import (
	"github.com/weiloon1234/Foundry-Go/pubsub"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

func invalid(brokers *pubsub.Brokers, name websocket.ConnectionName) { _, _ = brokers.Broker(name) }
