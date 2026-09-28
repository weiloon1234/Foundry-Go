package invalid

import (
	"context"
	"foundry.test/consumer/multifactor"
	"foundry.test/consumer/recovering"
	"github.com/weiloon1234/Foundry-Go/auth/mfa"
)

func wrong(ctx context.Context, factors *multifactor.Factors, password multifactor.PasswordResult, id mfa.EnrollmentID[recovering.Member], code mfa.TOTPCode) {
	_, _ = factors.Confirm(ctx, password, id, code)
}
