package compilefail

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/observability"
)

var _, _ = observability.New(observability.DefaultConfig(), func(context.Context, error) error { return nil })
