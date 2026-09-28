package auth

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/model"
)

// Reference returns the provider's generated key metadata. A reference is not
// authentication evidence. Framework adapters reuse it instead of another codec.
func (p Provider[M, K]) Reference() model.Reference[M, K] {
	if p.definition == nil {
		return model.Reference[M, K]{}
	}
	return p.definition.reference
}

// ValidateGuard requires the very same provider declaration, not merely matching
// names. It prevents adapters from pairing a guard with another lookup authority.
func (p Provider[M, K]) ValidateGuard(guard Guard[M]) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if err := guard.Validate(); err != nil {
		return err
	}
	if guard.definition.providerID != p.definition.id {
		return fault.New(fault.Invalid, "guard must share the provider declaration")
	}
	return nil
}

// Resolve reloads a typed reference and checks current eligibility. This is a
// server-side lookup, not credential verification or authorization. Missing and
// ineligible subjects return Unauthenticated. Callers bound its context and own
// the lifetime; an uncooperative lookup is never abandoned while still running.
func (p Provider[M, K]) Resolve(ctx context.Context, reference model.Reference[M, K]) (M, error) {
	if err := p.Validate(); err != nil {
		return *new(M), err
	}
	if ctx == nil {
		return *new(M), fault.New(fault.Invalid, "provider lookup requires a context")
	}
	var result M
	err := callback.Isolated("resolve authentication model", func() error {
		identity, err := reference.Identity()
		if err != nil {
			return err
		}
		result, err = p.resolve(ctx, identity)
		return err
	})
	if err != nil {
		return *new(M), err
	}
	if err := ctx.Err(); err != nil {
		return *new(M), err
	}
	return result, nil
}
