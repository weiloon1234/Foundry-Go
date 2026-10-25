package invalid

import (
	"context"
	a "foundry.test/goway/models/extensions"
	"github.com/weiloon1234/Foundry-Go/metadata"
)

func invalid(manager *metadata.Manager, key metadata.Key[a.Article, int64, string]) {
	_, _ = metadata.Matching(context.Background(), manager, key, 42)
}
