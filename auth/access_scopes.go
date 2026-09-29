package auth

import (
	"context"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/model"
)

// AccessScopeName is a persisted credential capability, not a model permission.
type AccessScopeName string

// MaxAccessScopes bounds both declarations and restored credential grants.
const MaxAccessScopes = 128

// AccessScope declares a credential capability owned by one subject model.
// Scope checks restrict credentials; policies still authorize current resources.
type AccessScope[M any] struct {
	_    [0]*M
	name AccessScopeName
}

func DefineAccessScope[M any](name AccessScopeName) AccessScope[M] {
	return AccessScope[M]{name: name}
}
func (s AccessScope[M]) Name() AccessScopeName { return s.name }
func (s AccessScope[M]) Validate() error {
	if !identifier.Semantic(string(s.name)) {
		return fault.New(fault.Invalid, "access scope requires a semantic name without wildcards")
	}
	return nil
}

type accessGrant struct{ names []AccessScopeName }

// AccessScopes is an immutable set owned by one subject model. Zero grants
// nothing. Names returns a copy; caller mutations cannot change a verified proof.
type AccessScopes[M any] struct {
	_     [0]*M
	grant *accessGrant
}

func NewAccessScopes[M any](scopes ...AccessScope[M]) (AccessScopes[M], error) {
	if len(scopes) > MaxAccessScopes {
		return AccessScopes[M]{}, fault.New(fault.Invalid, "access scope capacity exceeded")
	}
	names := make([]AccessScopeName, len(scopes))
	for i, scope := range scopes {
		if err := scope.Validate(); err != nil {
			return AccessScopes[M]{}, err
		}
		names[i] = scope.Name()
	}
	slices.Sort(names)
	for i := 1; i < len(names); i++ {
		if names[i] == names[i-1] {
			return AccessScopes[M]{}, fault.New(fault.Duplicate, "access scope is repeated")
		}
	}
	return AccessScopes[M]{grant: &accessGrant{names: names}}, nil
}
func (s AccessScopes[M]) names() []AccessScopeName {
	if s.grant == nil {
		return nil
	}
	return s.grant.names
}
func (s AccessScopes[M]) Names() []AccessScopeName { return slices.Clone(s.names()) }
func (s AccessScopes[M]) Len() int                 { return len(s.names()) }
func (s AccessScopes[M]) Contains(scope AccessScope[M]) bool {
	if scope.Validate() != nil {
		return false
	}
	_, found := slices.BinarySearch(s.names(), scope.Name())
	return found
}

// ContainsAll reports set inclusion; every set includes the empty set. A guard
// requirement separately rejects an empty set to prevent an accidental no-op.
func (s AccessScopes[M]) ContainsAll(required AccessScopes[M]) bool {
	for _, name := range required.names() {
		if _, found := slices.BinarySearch(s.names(), name); !found {
			return false
		}
	}
	return true
}

// Intersect returns the scopes present in both sets. Credential adapters use
// it to apply a binding's current ceiling to stored grants: removing a scope
// from the ceiling withdraws it from existing credentials without making them
// unreadable. The result is never larger than either input.
func (s AccessScopes[M]) Intersect(other AccessScopes[M]) AccessScopes[M] {
	names := make([]AccessScopeName, 0, min(s.Len(), other.Len()))
	for _, name := range s.names() {
		if _, found := slices.BinarySearch(other.names(), name); found {
			names = append(names, name)
		}
	}
	return AccessScopes[M]{grant: &accessGrant{names: names}}
}

// NewScopedProof is a trusted strategy boundary after credential verification.
// The immutable grants came from the authoritative credential store, never from
// unverified request scope names. Empty grants remain an explicitly scoped proof.
func NewScopedProof[M, K any](reference model.Reference[M, K], assurance Assurance, scopes AccessScopes[M]) (Proof[M, K], error) {
	proof, err := NewProof(reference, assurance)
	if err != nil {
		return Proof[M, K]{}, err
	}
	proof.grants = scopes.grant
	if proof.grants == nil {
		proof.grants = &accessGrant{}
	}
	return proof, nil
}

// AccessScopes returns verified credential restrictions and whether the proof
// carries scope grants. Unscoped proofs cannot satisfy RequireScopes.
func (p Proof[M, K]) AccessScopes() (AccessScopes[M], bool) {
	return AccessScopes[M]{grant: p.grants}, p.grants != nil
}

// RequireScopes returns the concrete model only when the credential explicitly
// grants every required scope. It rejects empty requirements, missing grants and
// pending MFA. Resolution shares Require/Optional's once-per-request model lookup.
// This does not replace a current model/resource policy decision.
func (g Guard[M]) RequireScopes(ctx context.Context, required AccessScopes[M]) (M, error) {
	if required.Len() == 0 {
		return *new(M), fault.New(fault.Invalid, "scope requirement cannot be empty")
	}
	resolved, err := g.resolve(ctx)
	if err != nil {
		return *new(M), err
	}
	subject, present := resolved.subject.Get()
	if !present {
		return *new(M), Unauthenticated
	}
	if resolved.grants == nil || !(AccessScopes[M]{grant: resolved.grants}).ContainsAll(required) {
		return *new(M), Forbidden
	}
	return subject, nil
}
