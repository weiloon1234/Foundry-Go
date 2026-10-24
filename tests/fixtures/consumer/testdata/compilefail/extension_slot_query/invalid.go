package invalid

import "foundry.test/consumer/articles"

// An article slot loads only on article queries.
var _ = articles.Authors.Query().With(articles.ArticleSlots.Title.Relation(articles.Articles.Title))
