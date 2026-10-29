package probe

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/http/modelbinding"
)

type User struct{ ID int64 }

var Users = orm.Define("users", func(b orm.Builder[User], m *User) orm.Table[User, int64] { return orm.TableOf(b, &m.ID) })

func Resolve(ctx context.Context, db database.Executor, id int64) (User, error) {
	return modelbinding.Resolve(modelbinding.ByKey(db, orm.Keyed(Users.Query(), Users.Key()), func(key int64) int64 { return key }), ctx, id)
}
