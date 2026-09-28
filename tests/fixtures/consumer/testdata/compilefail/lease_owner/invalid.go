package invalid

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/lease"
)

func wrong(b lease.Backend, k lease.Key) { _, _ = b.LeaseRelease(context.Background(), k, "owner") }
