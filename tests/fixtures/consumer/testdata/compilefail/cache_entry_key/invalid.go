package invalid

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/cache"
)

func wrong(b cache.Backend) { _, _, _ = b.Get(context.Background(), "raw-key") }
