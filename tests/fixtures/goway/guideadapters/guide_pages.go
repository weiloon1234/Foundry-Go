package guideadapters

import (
	"context"

	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

// doc-example:begin model-references-with-origin
func WithOrigin(ctx context.Context, subject model.Identifiable) (context.Context, error) {
	origin, err := (attribution.Origin{}).WithModel(subject)
	if err != nil {
		return nil, err
	}
	origin, err = origin.WithGuard("session")
	if err != nil {
		return nil, err
	}
	return attribution.WithContext(ctx, origin)
}

// doc-example:end model-references-with-origin

// doc-example:begin model-pagination-numbered
func numbered(ctx context.Context, db database.Executor) (orm.PageResult[models.User], error) {
	return orm.Paginate(ctx, db, models.Users.Query().Where(orm.Gte(models.Users.Age, 18)), orm.Page(1, 20))
}

func simple(ctx context.Context, db database.Executor) (orm.SimplePage[models.User], error) {
	return orm.SimplePaginate(ctx, db, models.Users.Query().Where(orm.Gte(models.Users.Age, 18)), orm.Page(1, 20))
}

// doc-example:end model-pagination-numbered

// doc-example:begin model-pagination-cursor-page
func cursorPage(ctx context.Context, db database.Executor, request orm.CursorRequest[models.User]) (orm.CursorPage[models.User], error) {
	q := models.Users.Query().OrderBy(orm.Asc(models.Users.Email))
	return orm.CursorPaginate(ctx, db, q, request)
}

// doc-example:end model-pagination-cursor-page
