package invalid

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/redis/raw"
)

func wrong(s *raw.Store) {
	_, _ = raw.NewCommand("GET", raw.DecodeString()).Run(context.Background(), s)
}
