package compilefail

import (
	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

func invalid(r *cli.Registry, i cli.Invocation, s cli.Streams) {
	_ = cli.Module("commands", foundation.NewKey[string]("commands"), r, i, s)
}
