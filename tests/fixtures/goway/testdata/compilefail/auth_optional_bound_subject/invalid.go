package invalid

import (
	"context"
	a "foundry.test/goway/models/identity"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	h "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/modelbinding"
	"github.com/weiloon1234/Foundry-Go/model"
)

func invalid(ex database.Executor, transport h.OptionalAuthenticationEndpoint[h.NoPath, h.NoQuery, h.NoBody, a.Member, h.NoContent]) {
	resolver := modelbinding.ByKey(ex, orm.Keyed(a.Others.Query(), a.Others.Key()), func(h.NoPath) model.ID[a.Other] { return model.ID[a.Other]{} })
	_ = modelbinding.BindAuthenticated(transport, resolver).Handle(func(context.Context, a.Member, modelbinding.Input[h.NoPath, h.NoQuery, h.NoBody, a.Other]) (h.NoContent, error) {
		return h.NoContent{}, nil
	})
}
