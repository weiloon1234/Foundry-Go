package extension_matching_old

import (
	"context"

	a "foundry.test/goway/models/extensions"
	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/metadata"
	"github.com/weiloon1234/Foundry-Go/translations"
)

func Files(ctx context.Context, m *attachments.Manager, c attachments.Collection[a.Article, int64]) (query.Predicate[a.Article], error) {
	return c.Matching(ctx, m)
}
func Text(ctx context.Context, m *translations.Manager, f translations.Field[a.Article, int64]) (query.Predicate[a.Article], error) {
	return f.Matching(ctx, m, "en", "x")
}
func Meta(ctx context.Context, m *metadata.Manager, k metadata.Key[a.Article, int64, string]) (query.Predicate[a.Article], error) {
	return k.Matching(ctx, m, "x")
}
