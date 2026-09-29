package token

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Current returns the metadata of the token that authenticated this guard in
// the current auth scope, including its effective scopes (for example to mark
// "this device" in List results by comparing IDs). It reuses the scope's
// verification; no second lookup runs.
func (t *Tokens[M, K]) Current(ctx context.Context) (Info[M, K], error) {
	if err := t.Validate(); err != nil {
		return Info[M, K]{}, err
	}
	return auth.CurrentCredential(ctx, t.guard, t.current)
}

// CurrentProof returns a proof for the current request's model that retains the
// presented token's scope grants. Issuing a token from it can only narrow those
// grants, unlike auth.NewProof, which is unrestricted.
func (t *Tokens[M, K]) CurrentProof(ctx context.Context) (auth.Proof[M, K], error) {
	if err := t.Validate(); err != nil {
		return auth.Proof[M, K]{}, err
	}
	return auth.CurrentProof(ctx, t.provider, t.guard)
}

// Logout revokes the family of a presented access token and reports EventLogout
// when it removed one.
func (t *Tokens[M, K]) Logout(ctx context.Context, access secret.String) (bool, error) {
	removed, err := t.Revoke(ctx, access)
	if err == nil && removed && t.observer != nil {
		event := auth.Event{Kind: auth.EventLogout, Guard: t.address.Guard, Provider: t.address.Provider}
		if info, current := auth.CurrentCredential(ctx, t.guard, t.current); current == nil {
			if identity, err := info.Subject().Identity(); err == nil {
				event.Subject = value.Set(identity)
			}
		}
		auth.Notify(ctx, t.observer, event)
	}
	return removed, err
}

// RevokeCurrent revokes the family of the token that authenticated this request
// (API logout on an authenticated route). It reports EventLogout when it
// removed the family.
func (t *Tokens[M, K]) RevokeCurrent(ctx context.Context) (bool, error) {
	info, err := t.Current(ctx)
	if err != nil {
		return false, err
	}
	removed, err := t.RevokeID(ctx, info.Subject(), info.ID())
	if err == nil && removed {
		t.notify(ctx, auth.EventLogout, info.Subject(), 0)
	}
	return removed, err
}

// RevokeOthers revokes every other token family of the current subject in this
// guard while keeping the current one ("log out other devices"). It runs under
// the subject lock shared with issuance and reports EventOtherDevicesLoggedOut.
func (t *Tokens[M, K]) RevokeOthers(ctx context.Context) (uint64, error) {
	info, err := t.Current(ctx)
	if err != nil {
		return 0, err
	}
	backend, ok := t.store.backend.(SelectiveBackend)
	if !ok {
		return 0, fault.New(fault.Invalid, "token backend cannot revoke other tokens")
	}
	var count uint64
	err = t.store.execute(ctx, func(op context.Context) error {
		identity, err := t.identity(info.Subject())
		if err != nil {
			return err
		}
		count, err = backend.RevokeOthers(op, t.address, identity, info.ID().value)
		return err
	})
	if err != nil {
		return 0, err
	}
	t.notify(ctx, auth.EventOtherDevicesLoggedOut, info.Subject(), count)
	return count, nil
}

// PruneExpired prunes expired token families of this guard in batches of batch
// families, stopping after a short batch or maxBatches, and reports the families
// removed. Schedule it from the application calendar or the configured
// housekeeping schedule; the auth package does not depend on the scheduler.
func (t *Tokens[M, K]) PruneExpired(ctx context.Context, batch, maxBatches int) (uint64, error) {
	if batch < 1 || batch > MaxPruneFamilies || maxBatches < 1 {
		return 0, fault.New(fault.Invalid, "invalid token prune bounds")
	}
	var total uint64
	for range maxBatches {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		removed, err := t.Prune(ctx, batch)
		total += removed
		if err != nil || removed < uint64(batch) {
			return total, err
		}
	}
	return total, nil
}
