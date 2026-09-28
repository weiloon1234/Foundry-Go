package compilefail

import (
	"context"
	"foundry.test/consumer/tooling"
	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

func invalid() {
	_, _ = tooling.Greet.Declare(func(foundation.Resolver) (cli.Handler[string], error) {
		return func(context.Context, string, cli.Streams) error { return nil }, nil
	})
}
