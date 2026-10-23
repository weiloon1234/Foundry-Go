package positive

import (
	"context"

	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/model"
)

// Static CI input only: lint must accept every ordinary typed operation even
// when concrete DB/Tx values are implicitly assigned to executor interfaces.
func Typed(ctx context.Context, db *database.DB, id model.ID[models.User]) {
	q := models.Users.Query().Where(models.Users.ID.Eq(id))
	users, _ := q.All(ctx, db)
	_, _ = q.First(ctx, db)
	_, _ = q.Count(ctx, db)
	all := q.All
	_, _ = all(ctx, db)
	_, _ = models.Users.Insert(ctx, db, models.Users.Email.Set("new"))
	_, _ = models.Users.Update(ctx, db, id, models.Users.Email.Set("updated"))
	_, _ = models.Users.Delete(ctx, db, id)
	_, _ = q.UpdateAll(ctx, db, models.Users.Age.Set(21))
	_, _ = q.DeleteAll(ctx, db)
	_, _ = models.Orders.Query().With(models.OrderRelations.Buyer).All(ctx, db)
	_, _ = orm.Load(ctx, db, users, models.UserRelations.Orders)
	_, _ = orm.CursorPaginate(ctx, db, q, query.CursorRequest[models.User]{Size: 10})
	_ = q.Chunk(ctx, db, 10, func([]models.User) error { return nil })
	_ = db.Transaction(ctx, func(tx *database.Tx) error {
		_, err := q.All(ctx, tx)
		if err != nil {
			return err
		}
		_, err = models.Users.Update(ctx, tx, id, models.Users.Email.Set("inside"))
		return err
	})
	var unused = db.Query
	_ = unused // Taking a raw function value is not a call.
}

// Concrete application methods are not framework raw entry points, even if the
// spelling and argument types happen to match an executor interface method.
type localService struct{}

func (localService) Query(context.Context, string, ...any) int { return 0 }
func Concrete(ctx context.Context)                             { _ = (localService{}).Query(ctx, "ordinary") }
