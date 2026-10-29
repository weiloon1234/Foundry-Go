package probe

import (
	"context"

	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

func Write(ctx context.Context, db *database.DB, id model.ID[models.User]) error {
	q := models.Users.Query().Where(orm.TextEq(models.Users.Email, "Ada"))
	_, err := orm.Update(ctx, db, q, models.Users.Key(), id, orm.TextSet(models.Users.Email, "Grace"))
	if err != nil {
		return err
	}
	_, err = orm.Delete(ctx, db, q, models.Users.Key(), id)
	return err
}
