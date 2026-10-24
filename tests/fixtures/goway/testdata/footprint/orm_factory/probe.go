package probe

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/testkit/factory"
)

type User struct{ ID int64 }

var Users = orm.Define("users", func(b orm.Builder[User], m *User) orm.Table[User, int64] { return orm.TableOf(b, &m.ID) })

func Create(ctx context.Context, db database.Transactor) ([]User, error) {
	f, err := factory.Define(Users, func(_ context.Context, n factory.Sequence) ([]orm.Assignment[User], error) {
		return []orm.Assignment[User]{Users.Key().Set(int64(n))}, nil
	})
	if err != nil {
		return nil, err
	}
	return f.CreateMany(ctx, db, 3)
}
