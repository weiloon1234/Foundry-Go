package invalid

import (
	"context"
	"foundry.test/consumer/passwords"
	"foundry.test/consumer/recovering"
	"github.com/weiloon1234/Foundry-Go/database"
)

func bad(ctx context.Context, tx *database.Tx, sessions *recovering.Sessions, account passwords.Account) {
	sessions.RevokeAllIn(ctx, tx, account.FoundryReference())
}
