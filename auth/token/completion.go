package token

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
)

// CompleteMFA replaces a live pending credential from THIS guard with one fresh
// authenticated credential. The lookup is only a locator; checked creation
// revalidates and consumes it with the current model and factor in one transaction.
// Full, expired, revoked, wrong-guard or already-consumed credentials are rejected.
// No reusable authenticated proof escapes, and failures return no new secrets.
// Options belong to the server's login policy, not unvalidated client claims.
func (t *Tokens[M, K]) CompleteMFA(ctx context.Context, raw secret.String, factor auth.SecondFactor[M, K], options IssueOptions[M]) (Issued[M, K], error) {
	if err := t.Validate(); err != nil {
		return Issued[M, K]{}, err
	}
	if err := factor.ValidateFor(t.provider); err != nil {
		return Issued[M, K]{}, err
	}
	backend, ok := t.store.backend.(CompletionBackend)
	if !ok {
		return Issued[M, K]{}, fault.New(fault.Invalid, "credential backend cannot complete MFA atomically")
	}
	var result Issued[M, K]
	err := t.store.execute(ctx, func(op context.Context) error {
		hash, err := HashSecret(raw)
		if err != nil {
			return err
		}
		found, err := t.store.backend.Lookup(op, t.address, hash, false)
		if err != nil {
			return err
		}
		pending, present := found.Get()
		if !present {
			return auth.Unauthenticated
		}
		if err := pending.Validate(t.address); err != nil {
			return err
		}
		if !pending.AccessHash.Equal(hash) {
			return fault.New(fault.Invalid, "credential lookup returned a different pending secret")
		}
		if pending.Assurance != auth.PendingMFA || pending.Mode != Challenge {
			return auth.Unauthenticated
		}
		reference, err := t.provider.Parse(pending.Subject)
		if err != nil {
			return err
		}
		proof, err := auth.NewProof(reference, auth.Authenticated)
		if err != nil {
			return err
		}
		proof, err = proof.WithIssuanceCheck(func(inner context.Context, tx *database.Tx) error {
			return factor.Check(inner, tx, t.provider, pending.Subject, func(check context.Context, child *database.Tx) error {
				return backend.ConsumePendingIn(check, child, t.address, pending)
			})
		})
		if err != nil {
			return err
		}
		result, err = t.issue(op, proof, options, value.Set(pending.AccessExpiresAt))
		return err
	})
	if err != nil {
		return Issued[M, K]{}, err
	}
	t.issued(ctx, result)
	return result, nil
}
