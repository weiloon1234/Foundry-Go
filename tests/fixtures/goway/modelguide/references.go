package modelguide

import "github.com/weiloon1234/Foundry-Go/model"

// doc-example:begin generated-identity-parse
func RestoreArticle(article Article) (model.Reference[Article, model.ID[Article]], error) {
	identity, err := article.FoundryIdentity()
	if err != nil {
		return model.Reference[Article, model.ID[Article]]{}, err
	}
	return articlesIdentity.Parse(identity)
}

// doc-example:end generated-identity-parse
