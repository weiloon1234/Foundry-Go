package invalid

import "github.com/weiloon1234/Foundry-Go/infrastructure"

var invalid = infrastructure.SettingsConfigKeys().Database.Connections.Set(infrastructure.RedisConnections{})
