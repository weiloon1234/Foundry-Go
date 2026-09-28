package auth

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

type issuanceCheck struct {
	verify func(context.Context, *database.Tx) error
}

// WithIssuanceCheck binds trusted verification to its later persistence step.
// The check MUST lock and revalidate current domain state on tx. It runs before
// credential-store locks in the same transaction as creation, never afterward.
// A check cannot be replaced. The callback may be invoked by separate Issue
// calls and must be safe for concurrent use; it never commits or retries.
// This is a trusted adapter boundary, not a way to verify submitted identities.
func (p Proof[M, K]) WithIssuanceCheck(check func(context.Context, *database.Tx) error) (Proof[M, K], error) {
	if err := p.identity.Validate(); err != nil {
		return Proof[M, K]{}, err
	}
	if err := p.assurance.Validate(); err != nil {
		return Proof[M, K]{}, err
	}
	if check == nil || p.issuance != nil {
		return Proof[M, K]{}, fault.New(fault.Invalid, "proof requires one immutable issuance check")
	}
	p.issuance = &issuanceCheck{verify: check}
	return p, nil
}
func (p Proof[M, K]) HasIssuanceCheck() bool { return p.issuance != nil }

// CheckIssuance is called by a transactional credential backend. The caller owns
// the transaction, context timeout and operation capacity. Failures preserve
// error identity while isolating panic/Goexit. A checked proof cannot be persisted
// by an adapter without the corresponding atomic creation capability.
func (p Proof[M, K]) CheckIssuance(ctx context.Context, tx *database.Tx) error {
	if ctx == nil || tx == nil {
		return fault.New(fault.Invalid, "proof check requires a context and transaction")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.issuance == nil {
		return nil
	}
	err := callback.Isolated("credential issuance check", func() error { return p.issuance.verify(ctx, tx) })
	if err != nil {
		return err
	}
	return ctx.Err()
}

// WithAccessScopes narrows a verified proof while preserving its issuance check.
// It cannot expand an existing grant. Use this instead of reconstructing a new
// proof from its identity, which would discard verification provenance.
func (p Proof[M, K]) WithAccessScopes(scopes AccessScopes[M]) (Proof[M, K], error) {
	if err := p.identity.Validate(); err != nil {
		return Proof[M, K]{}, err
	}
	if err := p.assurance.Validate(); err != nil {
		return Proof[M, K]{}, err
	}
	if old, scoped := p.AccessScopes(); scoped && !old.ContainsAll(scopes) {
		return Proof[M, K]{}, Forbidden
	}
	p.grants = scopes.grant
	if p.grants == nil {
		p.grants = &accessGrant{}
	}
	return p, nil
}
