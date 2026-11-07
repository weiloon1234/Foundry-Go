package articles

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/seed"
	"github.com/weiloon1234/Foundry-Go/model"
)

// doc-example:begin seed-reference-data
func AuthorSeeder(id model.ID[Author], name string) seed.Definition {
	return seed.Definition{ID: "articles.author", Run: func(ctx context.Context, tx *database.Tx) error {
		_, err := orm.Upsert(ctx, tx, Authors.Query(),
			orm.OnConflict(orm.ConflictKey(orm.KeyExpr(Authors.Key()))).DoNothing(),
			orm.KeySet(Authors.Key(), id), orm.TextSet(Authors.Name, name))
		return err
	}}
}

func SeedAuthors(ctx context.Context, db *database.DB, id model.ID[Author], name string) (seed.Result, error) {
	registry, err := seed.New(AuthorSeeder(id, name))
	if err != nil {
		return seed.Result{}, err
	}
	return registry.Run(ctx, db, "articles.author")
}

// doc-example:end seed-reference-data
