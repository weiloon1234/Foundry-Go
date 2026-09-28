package auth

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

// RecheckPassword locks and returns the current model using the original
// PasswordLogin binding. It rechecks identity, the verified hash, eligibility
// and MFA policy without performing a second provider lookup or password hash.
// A result from a different provider declaration is rejected even when names
// and model types match. Errors return a zero model.
//
// Call this before factor/credential-store locks in the transaction owning a
// sensitive password-authorized change. The caller supplies operation capacity,
// timeout and transaction ownership. Keep the result request-local: this method
// checks current model state, not a persisted reauthentication ticket's age.
// PendingMFA results verify only the password stage; an existing second factor
// is still required where the protected operation demands it.
func (p Provider[M, K]) RecheckPassword(ctx context.Context, tx *database.Tx, result PasswordResult[M, K]) (M, error) {
	if err := p.Validate(); err != nil {
		return *new(M), err
	}
	if ctx == nil || tx == nil {
		return *new(M), fault.New(fault.Invalid, "password recheck requires a context and transaction")
	}
	if err := ctx.Err(); err != nil {
		return *new(M), err
	}
	if result.recheck == nil || result.recheck.provider != p.definition.id || result.recheck.verify == nil {
		return *new(M), fault.New(fault.Invalid, "password result does not belong to this provider declaration")
	}
	var subject M
	err := callback.Isolated("password model recheck", func() error {
		var err error
		subject, err = result.recheck.verify(ctx, tx)
		return err
	})
	if err != nil {
		return *new(M), err
	}
	if err := ctx.Err(); err != nil {
		return *new(M), err
	}
	return subject, nil
}
