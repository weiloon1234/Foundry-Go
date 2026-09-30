package invalid

import (
	"context"

	"foundry.test/consumer/articles"
)

// A metadata slot accepts only its declared value type.
func invalid(article articles.Article) {
	_ = articles.ArticleExtensions().SEO.Save(context.Background(), article, "canonical")
}
