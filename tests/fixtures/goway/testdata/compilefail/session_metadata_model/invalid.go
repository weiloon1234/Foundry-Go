package invalid

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

func bad(ctx context.Context, sessions *session.Sessions[user, model.ID[user]]) {
	var info session.Info[models.Order, model.ID[models.Order]]
	rows, _ := sessions.List(ctx, orm.IdentityOf(users, users.Key()).ReferenceOf((user{})))
	info = rows[0]
	_ = info
}

// The service's Identifiable contract is supplied by the ORM declaration.
type user struct{ ID model.ID[user] }

var users = orm.Define("users", func(b orm.Builder[user], m *user) orm.Table[user, model.ID[user]] { return orm.TableOf(b, &m.ID) })

func (u user) FoundryIdentity() (model.Identity, error) {
	return orm.IdentityOf(users, users.Key()).ReferenceOf(u).Identity()
}
