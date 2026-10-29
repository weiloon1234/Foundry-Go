package invalid

import "github.com/weiloon1234/Foundry-Go/validation"

import "foundry.test/consumer/articles"

// A translated-input rule applies only to a locale-keyed map field.
var _ = validation.Rules(articles.ArticleInputValidationFields().Slug, articles.ArticleSlots.Title.Rule())
