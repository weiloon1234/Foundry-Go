package invalid

import (
	"context"
	a "foundry.test/goway/models/extensions"
	"github.com/weiloon1234/Foundry-Go/translations"
)

func invalid(manager *translations.Manager, field translations.Field[a.Article, int64]) {
	predicate, _ := translations.Matching(context.Background(), manager, field, "en", "x")
	_ = a.Authors.Query().Where(predicate)
}
