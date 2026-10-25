package invalid

import (
	a "foundry.test/goway/models/identity"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	h "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/modelbinding"
	"github.com/weiloon1234/Foundry-Go/model"
)

func invalid(ex database.Executor, transport h.Endpoint[h.NoPath, h.NoQuery, h.NoBody, h.NoContent]) {
	resolver := modelbinding.ByKey(ex, orm.Keyed(a.Members.Query(), a.Members.Key()), func(h.NoPath) model.ID[a.Member] { return model.ID[a.Member]{} })
	_ = modelbinding.Bind[h.NoPath, h.NoQuery, h.NoBody, a.Other, h.NoContent](transport, resolver)
}
