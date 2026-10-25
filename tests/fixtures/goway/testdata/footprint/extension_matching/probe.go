package extension_matching

import (
	"context"

	a "foundry.test/goway/models/extensions"
	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/metadata"
	"github.com/weiloon1234/Foundry-Go/translations"
)

func Files(ctx context.Context, m *attachments.Manager, c attachments.Collection[a.Article, int64]) (orm.Predicate[a.Article], error) {
	return attachments.Matching(ctx, m, c)
}
func Text(ctx context.Context, m *translations.Manager, f translations.Field[a.Article, int64]) (orm.Predicate[a.Article], error) {
	return translations.Matching(ctx, m, f, "en", "x")
}
func Meta(ctx context.Context, m *metadata.Manager, k metadata.Key[a.Article, int64, string]) (orm.Predicate[a.Article], error) {
	return metadata.Matching(ctx, m, k, "x")
}
