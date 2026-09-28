package compilefail

import (
	"github.com/weiloon1234/Foundry-Go/foundation"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/plugin"
)

var _ = plugin.RegisterMigrations(nil, foundation.NewKey[*foundryhttp.Router]("wrong"))
