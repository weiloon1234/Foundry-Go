package invalid

import (
	"context"
	"foundry.test/consumer/multifactor"
	"github.com/weiloon1234/Foundry-Go/auth/mfa"
)

func wrong(ctx context.Context, factors *multifactor.Factors, password multifactor.PasswordResult, id mfa.EnrollmentID[multifactor.Account], code mfa.RecoveryCode) {
	_, _ = factors.Confirm(ctx, password, id, code)
}
