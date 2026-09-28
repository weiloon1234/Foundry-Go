package invalid

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/redis/raw"
)

func wrong(c raw.Command[int64], s *raw.Store) (string, error) { return c.Run(context.Background(), s) }
