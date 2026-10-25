package invalid

import (
	"context"
	a "foundry.test/goway/models/extensions"
	"github.com/weiloon1234/Foundry-Go/translations"
)

func invalid(manager *translations.Manager, field translations.Field[a.Article, int64], raw string) {
	_, _ = translations.Matching(context.Background(), manager, field, raw, "x")
}
