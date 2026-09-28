package invalid

import (
	"context"
	"foundry.test/consumer/recovering"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/model"
)

func bad(provider recovering.Provider, name auth.GuardName) {
	auth.DefineRevocation(name, provider, func(context.Context, *database.Tx, model.Reference[recovering.Member, model.ID[recovering.Member]]) (uint64, error) {
		return 0, nil
	})
}
