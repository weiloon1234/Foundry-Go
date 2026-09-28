package compilefail

import "github.com/weiloon1234/Foundry-Go/database/postgres"

var _ = postgres.RoutingConfig{Read: postgres.DefaultConfig()}
