package invalid

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

func bad(ctx context.Context, tokens *token.Tokens[user, model.ID[user]], user user, id token.ID[models.Order]) {
	_, _ = tokens.RevokeID(ctx, orm.IdentityOf(users, users.Key()).ReferenceOf(user), id)
}

// The service's Identifiable contract is supplied by the ORM declaration.
type user struct{ ID model.ID[user] }

var users = orm.Define("users", func(b orm.Builder[user], m *user) orm.Table[user, model.ID[user]] { return orm.TableOf(b, &m.ID) })

func (u user) FoundryIdentity() (model.Identity, error) {
	return orm.IdentityOf(users, users.Key()).ReferenceOf(u).Identity()
}
