package invalid

import (
	"github.com/weiloon1234/Foundry-Go/events"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

func invalid(r *foundation.Registrar, bus foundation.Key[*events.Bus]) {
	_ = events.RegisterOutbox(r, foundation.NewKey[*events.Bus]("wrong"), bus, "destination")
}
