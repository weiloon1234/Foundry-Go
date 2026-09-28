package compilefail

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/health"
)

var _ = health.Probe{ID: "database", Check: func(context.Context) bool { return true }}
