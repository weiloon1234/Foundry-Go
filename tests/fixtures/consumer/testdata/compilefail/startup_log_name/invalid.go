package invalid

import (
	"github.com/weiloon1234/Foundry-Go/logging"
	"github.com/weiloon1234/Foundry-Go/pubsub"
)

func invalid(channels *logging.Channels, name pubsub.ConnectionName) { _, _ = channels.Channel(name) }
