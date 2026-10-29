package positive

import (
	"context"

	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

// Static CI input only: lint must accept every ordinary typed operation even
// when concrete DB/Tx values are implicitly assigned to executor interfaces.
func Typed(ctx context.Context, db *database.DB, id model.ID[models.User]) {
	q := models.Users.Query().Where(orm.KeyEq(models.Users.ID, id))
	users, _ := orm.All(ctx, db, q)
	_, _ = orm.First(ctx, db, q)
	_, _ = orm.Count(ctx, db, q)
	all := func(ctx context.Context, ex database.Executor) ([]models.User, error) {
		return orm.All(ctx, ex, q)
	}
	_, _ = all(ctx, db)
	_, _ = orm.Insert(ctx, db, models.Users.Table, orm.TextSet(models.Users.Email, "new"))
	_, _ = orm.Update(ctx, db, models.Users.Table.Query(), models.Users.Table.Key(), id, orm.TextSet(models.Users.Email, "updated"))
	_, _ = orm.Delete(ctx, db, models.Users.Table.Query(), models.Users.Table.Key(), id)
	_, _ = orm.UpdateAll(ctx, db, q, orm.NumberSet(models.Users.Age, 21))
	_, _ = orm.DeleteAll(ctx, db, q)
	_, _ = orm.All(ctx, db, models.Orders.Query().With(models.OrderRelations.Buyer))
	_, _ = orm.LoadFrom(ctx, db, models.Users.Query().With(models.UserRelations.Orders), users)
	_, _ = orm.CursorPaginate(ctx, db, q, orm.CursorRequest[models.User]{Size: 10})
	_ = orm.Chunk(ctx, db, q, 10, func([]models.User) error { return nil })
	_ = db.Transaction(ctx, func(tx *database.Tx) error {
		_, err := orm.All(ctx, tx, q)
		if err != nil {
			return err
		}
		_, err = orm.Update(ctx, tx, models.Users.Table.Query(), models.Users.Table.Key(), id, orm.TextSet(models.Users.Email, "inside"))
		return err
	})
	var unused = db.Query
	_ = unused // Taking a raw function value is not a call.
	unusedRaw := orm.Raw[models.User, model.ID[models.User]]
	_ = unusedRaw
}

// Concrete application methods are not framework raw entry points, even if the
// spelling and argument types happen to match an executor interface method.
type localService struct{}

func (localService) Query(context.Context, string, ...any) int { return 0 }
func Concrete(ctx context.Context)                             { _ = (localService{}).Query(ctx, "ordinary") }
