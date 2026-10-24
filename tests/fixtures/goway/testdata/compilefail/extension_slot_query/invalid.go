package invalid

import a "foundry.test/goway/models/extensions"

var _ = a.Authors.Query().With(a.ArticleExtensions.Title.Relation(a.Articles.Title))
