package invalid

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/audit"
)

func invalid(ctx context.Context) { _, _ = audit.WithArea(ctx, attribution.GuardName("admin")) }
