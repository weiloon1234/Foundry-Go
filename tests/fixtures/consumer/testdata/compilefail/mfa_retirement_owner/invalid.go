package invalid

import (
	"context"
	"foundry.test/consumer/models"
	"foundry.test/consumer/multifactor"
	"github.com/weiloon1234/Foundry-Go/database"
)

func invalid(ctx context.Context, tx *database.Tx, f *multifactor.Factors, u models.User) {
	_, _ = f.RetireIn(ctx, tx, u.FoundryReference())
}
