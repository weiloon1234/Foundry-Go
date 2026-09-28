package compilefail

import (
	"foundry.test/consumer/tooling"
	"github.com/weiloon1234/Foundry-Go/cli"
)

func invalid(name string, decode cli.Decoder[tooling.Greeting]) {
	_ = cli.Define(name, "greeting", decode)
}
