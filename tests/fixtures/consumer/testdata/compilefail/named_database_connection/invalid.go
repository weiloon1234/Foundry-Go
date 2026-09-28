package invalid

import (
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/redis"
)

func invalid(connections *database.Connections) {
	_, _ = connections.Connection(redis.ConnectionName("default"))
}
