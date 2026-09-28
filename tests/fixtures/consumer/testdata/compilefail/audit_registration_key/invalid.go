package invalid

import (
	"github.com/weiloon1234/Foundry-Go/audit"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/events"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

func invalid(r *foundation.Registrar, pool foundation.Key[*database.DB], wrong foundation.Key[*events.Bus]) {
	_ = audit.Register(r, pool, wrong, audit.DefaultConfig())
}
