package invalid

import (
	"context"
	"foundry.test/consumer/multifactor"
	"foundry.test/consumer/recovering"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/model"
)

func wrong(ctx context.Context, factors *multifactor.Factors, password auth.PasswordResult[recovering.Member, model.ID[recovering.Member]]) {
	_, _ = factors.Enroll(ctx, password)
}
