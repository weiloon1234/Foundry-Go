package invalid

import (
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
)

func bad(s application.Settings) { _, _ = s.WithDatabaseScopes(infrastructure.Settings{}) }
