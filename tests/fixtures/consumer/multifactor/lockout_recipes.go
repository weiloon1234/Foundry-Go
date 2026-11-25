package multifactor

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth/lockout"
	"github.com/weiloon1234/Foundry-Go/auth/mfa"
	"github.com/weiloon1234/Foundry-Go/model"
)

// doc-example:begin throttle-factor-check
func throttleFactorCheck(ctx context.Context, throttle lockout.Throttle[model.ID[Account]], key model.ID[Account], submittedCode mfa.TOTPCode, verifyFactor func(context.Context, mfa.TOTPCode) (bool, error)) (bool, error) {
	verified, err := throttle.Run(ctx, key, func(op context.Context) (bool, error) {
		return verifyFactor(op, submittedCode)
	})
	// Only a true result with no error may grant authority or issue credentials.
	return verified, err
}

// doc-example:end throttle-factor-check
