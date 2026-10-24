package probe

import (
	"context"
	e "foundry.test/goway/oldmodels/extensions"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/extensions/slots"
)

func Load(ctx context.Context, db database.Executor, runtime slots.Runtime, parents []e.Article) ([]e.Article, error) {
	return e.QueryExtensionArticles().With(e.ArticleExtensions().From(runtime).All()...).LoadMissing(ctx, db, parents)
}
