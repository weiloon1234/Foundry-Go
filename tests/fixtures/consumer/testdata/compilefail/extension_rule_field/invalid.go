package invalid

import "foundry.test/consumer/articles"

// A translated-input rule applies only to a locale-keyed map field.
var _ = articles.ArticleInputValidationFields().Slug.Rules(articles.ArticleExtensions().Title.Rule())
