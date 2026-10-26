package probe

import (
	"context"
	e "foundry.test/goway/models/extensions"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/extensions/slots"
)

func Load(ctx context.Context, db database.Executor, runtime slots.Runtime, parents []e.Article) ([]e.Article, error) {
	return orm.LoadMissingFrom(ctx, db, e.Articles.Query().With(e.ArticleExtensions.From(runtime).All()...), parents)
}
