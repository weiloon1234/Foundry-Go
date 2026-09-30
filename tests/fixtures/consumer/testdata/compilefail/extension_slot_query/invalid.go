package invalid

import "foundry.test/consumer/articles"

// An article slot loads only on article queries.
var _ = articles.QueryArticleAuthors().With(articles.ArticleExtensions().Title)
