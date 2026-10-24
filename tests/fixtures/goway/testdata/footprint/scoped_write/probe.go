package probe

import (
	"context"

	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

func Write(ctx context.Context, db *database.DB, id model.ID[models.User]) error {
	q := models.Users.Query().Where(models.Users.Email.Eq("Ada"))
	_, err := orm.Update(ctx, db, q, models.Users.Key(), id, models.Users.Email.Set("Grace"))
	if err != nil {
		return err
	}
	_, err = orm.Delete(ctx, db, q, models.Users.Key(), id)
	return err
}
