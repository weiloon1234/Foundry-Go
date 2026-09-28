package compilefail

import "github.com/weiloon1234/Foundry-Go/foundation"

var _ = foundation.NewBuilder().RegisterPlugin(foundation.Module{Name: "legacy"})
