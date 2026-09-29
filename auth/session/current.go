package session

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
)

// Current returns the metadata of the session that authenticated this guard in
// the current auth scope (for example to mark "this device" in List results by
// comparing IDs). It reuses the scope's verification; no second lookup runs.
func (s *Sessions[M, K]) Current(ctx context.Context) (Info[M, K], error) {
	if err := s.Validate(); err != nil {
		return Info[M, K]{}, err
	}
	return auth.CurrentCredential(ctx, s.guard, s.current)
}

// CurrentProof returns a proof for the current request's model, for issuing a
// related credential (for example an API token) without constructing an
// unrestricted proof from a submitted identity. Sessions carry no scope grants.
// An impersonation session is refused with auth.ImpersonationForbidden: the
// impersonator must not mint an unmarked token or session for the subject.
func (s *Sessions[M, K]) CurrentProof(ctx context.Context) (auth.Proof[M, K], error) {
	if err := s.Validate(); err != nil {
		return auth.Proof[M, K]{}, err
	}
	return auth.CurrentProof(ctx, s.provider, s.guard)
}

// Logout revokes a presented session secret and reports EventLogout when it
// removed a session. Browser adapters use it for the cookie's own logout.
func (s *Sessions[M, K]) Logout(ctx context.Context, credential secret.String) (bool, error) {
	removed, err := s.Revoke(ctx, credential)
	if err == nil && removed && s.observer != nil {
		if info, current := auth.CurrentCredential(ctx, s.guard, s.current); current == nil {
			s.notify(ctx, auth.EventLogout, info, 0)
		} else {
			auth.Notify(ctx, s.observer, auth.Event{Kind: auth.EventLogout, Guard: s.address.Guard, Provider: s.address.Provider})
		}
	}
	return removed, err
}

// RevokeCurrent revokes the session that authenticated this request, for API
// logout on an authenticated route. It reports EventLogout when it removed it.
func (s *Sessions[M, K]) RevokeCurrent(ctx context.Context) (bool, error) {
	info, err := s.Current(ctx)
	if err != nil {
		return false, err
	}
	removed, err := s.RevokeID(ctx, info.Subject(), info.ID())
	if err == nil && removed {
		s.notify(ctx, auth.EventLogout, info, 0)
	}
	return removed, err
}

// RevokeOthers revokes every other session of the current subject in this
// guard while keeping the current one ("log out other devices"). It runs under
// the subject lock shared with issuance and reports EventOtherDevicesLoggedOut.
// Combine with the model's other credential stores for a full sign-out.
func (s *Sessions[M, K]) RevokeOthers(ctx context.Context) (uint64, error) {
	info, err := s.Current(ctx)
	if err != nil {
		return 0, err
	}
	if info.Impersonated() {
		// Logging the subject out everywhere is the subject's own decision.
		return 0, auth.ImpersonationForbidden
	}
	backend, ok := s.store.backend.(SelectiveBackend)
	if !ok {
		return 0, fault.New(fault.Invalid, "session backend cannot revoke other sessions")
	}
	var count uint64
	err = s.store.execute(ctx, func(op context.Context) error {
		identity, err := info.Subject().Identity()
		if err != nil {
			return err
		}
		// The revocation has committed: its count is reported, never turned
		// into an error that would hide a completed logout.
		count, err = backend.RevokeOthers(op, s.address, identity, info.ID().value)
		return err
	})
	if err != nil {
		return 0, err
	}
	s.notify(ctx, auth.EventOtherDevicesLoggedOut, info, count)
	return count, nil
}

// ConfirmCurrent records that the holder of the current session re-entered its
// password now. Call it only after verifying the password (for example with
// auth.PasswordLogin.Confirm); RequireConfirmed then admits sensitive actions.
func (s *Sessions[M, K]) ConfirmCurrent(ctx context.Context) (Info[M, K], error) {
	info, err := s.Current(ctx)
	if err != nil {
		return Info[M, K]{}, err
	}
	if info.Impersonated() {
		// The impersonator does not know the subject's password; an impersonated
		// session can never pass a password confirmation.
		return Info[M, K]{}, auth.ImpersonationForbidden
	}
	backend, ok := s.store.backend.(ConfirmationBackend)
	if !ok {
		return Info[M, K]{}, fault.New(fault.Invalid, "session backend cannot record password confirmation")
	}
	var result Info[M, K]
	err = s.store.execute(ctx, func(op context.Context) error {
		identity, err := info.Subject().Identity()
		if err != nil {
			return err
		}
		found, err := backend.Confirm(op, s.address, identity, info.ID().value)
		if err != nil {
			return err
		}
		record, present := found.Get()
		if !present {
			return auth.Unauthenticated
		}
		if record.ID != info.ID().value || record.Subject != identity || !record.ConfirmedAt.IsSet() {
			return fault.New(fault.Invalid, "session backend confirmed a different session")
		}
		result, err = s.info(record)
		return err
	})
	if err != nil {
		return Info[M, K]{}, err
	}
	return result, nil
}

// RequireNotImpersonating returns auth.ImpersonationForbidden when the current
// session is an impersonation session. Call it before sensitive operations
// such as changing the password, email or MFA factors, or deleting the account.
func (s *Sessions[M, K]) RequireNotImpersonating(ctx context.Context) error {
	info, err := s.Current(ctx)
	if err != nil {
		return err
	}
	if info.Impersonated() {
		return auth.ImpersonationForbidden
	}
	return nil
}

// RequireConfirmed returns auth.ConfirmationRequired unless the current session
// confirmed its password within the given duration, checked against the
// backend's current time rather than the scope's cached metadata.
func (s *Sessions[M, K]) RequireConfirmed(ctx context.Context, within time.Duration) error {
	if within <= 0 {
		return fault.New(fault.Invalid, "password confirmation window must be positive")
	}
	info, err := s.Current(ctx)
	if err != nil {
		return err
	}
	if !info.ConfirmedAt().IsSet() {
		return auth.ConfirmationRequired
	}
	backend, ok := s.store.backend.(ConfirmationBackend)
	if !ok {
		return fault.New(fault.Invalid, "session backend cannot check password confirmation")
	}
	var recent bool
	err = s.store.execute(ctx, func(op context.Context) error {
		identity, err := info.Subject().Identity()
		if err != nil {
			return err
		}
		recent, err = backend.ConfirmedWithin(op, s.address, identity, info.ID().value, within)
		return err
	})
	if err != nil {
		return err
	}
	if !recent {
		return auth.ConfirmationRequired
	}
	return nil
}

// PruneExpired prunes expired sessions of this guard in batches of batch rows,
// stopping after a short batch or maxBatches, and reports the rows removed.
// Schedule it from the application calendar or the configured housekeeping
// schedule; the auth package does not depend on the scheduler.
func (s *Sessions[M, K]) PruneExpired(ctx context.Context, batch, maxBatches int) (uint64, error) {
	if batch < 1 || batch > MaxPageSize || maxBatches < 1 {
		return 0, fault.New(fault.Invalid, "invalid session prune bounds")
	}
	var total uint64
	for range maxBatches {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		removed, err := s.Prune(ctx, batch)
		total += removed
		if err != nil || removed < uint64(batch) {
			return total, err
		}
	}
	return total, nil
}
