package invalid

import (
	"context"
	a "foundry.test/goway/models/extensions"
)

func invalid(article a.Article) { _ = a.ArticleExtensions.SEO.Save(context.Background(), article, 123) }
