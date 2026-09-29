package auth

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
)

// CredentialSlot is a trusted credential adapter's typed channel from the proof
// it verified to code running in the same authentication scope, such as the
// session or token metadata behind the current request. Only holders of the slot
// value can attach or read it; attached metadata never becomes a claim, grant or
// authority, and a new scope verifies the credential again.
type CredentialSlot[C any] struct{ id *declarationID }

func NewCredentialSlot[C any]() CredentialSlot[C] { return CredentialSlot[C]{id: &declarationID{}} }
func (s CredentialSlot[C]) Validate() error {
	if s.id == nil {
		return fault.New(fault.Invalid, "credential slot is not declared")
	}
	return nil
}

type attachedCredential struct {
	slot  *declarationID
	value any // The slot's C; read back only through the same typed slot.
	// impersonated marks a credential issued by impersonation: it can act as
	// its subject but never mint another credential for it.
	impersonated bool
}

// AttachCredential returns a copy of a verified proof carrying credential
// metadata for its adapter's slot. It is a trusted adapter boundary: attach only
// metadata of the credential that was actually verified. A proof carries at
// most one attachment; grants and issuance checks are preserved.
func AttachCredential[M, K, C any](proof Proof[M, K], slot CredentialSlot[C], metadata C) (Proof[M, K], error) {
	return attach(proof, slot, metadata, false)
}

// AttachImpersonatedCredential is AttachCredential for a credential issued by
// impersonation. CurrentProof then refuses the scope with
// ImpersonationForbidden, so an impersonator can never mint an unmarked token
// or session for the subject.
func AttachImpersonatedCredential[M, K, C any](proof Proof[M, K], slot CredentialSlot[C], metadata C) (Proof[M, K], error) {
	return attach(proof, slot, metadata, true)
}

func attach[M, K, C any](proof Proof[M, K], slot CredentialSlot[C], metadata C, impersonated bool) (Proof[M, K], error) {
	if err := slot.Validate(); err != nil {
		return Proof[M, K]{}, err
	}
	if err := proof.identity.Validate(); err != nil {
		return Proof[M, K]{}, err
	}
	if err := proof.assurance.Validate(); err != nil {
		return Proof[M, K]{}, err
	}
	if proof.credential != nil {
		return Proof[M, K]{}, fault.New(fault.Duplicate, "proof already carries credential metadata")
	}
	proof.credential = &attachedCredential{slot: slot.id, value: metadata, impersonated: impersonated}
	return proof, nil
}

// CurrentCredential returns the metadata the guard's strategy attached for the
// credential that authenticated this scope. It reuses the scope's cached guard
// result: no second verification or lookup runs. An anonymous or pending scope
// fails like Require; a guard whose strategy uses another slot returns Missing.
func CurrentCredential[M, C any](ctx context.Context, guard Guard[M], slot CredentialSlot[C]) (C, error) {
	if err := slot.Validate(); err != nil {
		return *new(C), err
	}
	result, err := guard.resolve(ctx)
	if err != nil {
		return *new(C), err
	}
	if !result.subject.IsSet() {
		return *new(C), Unauthenticated
	}
	if result.credential == nil || result.credential.slot != slot.id {
		return *new(C), fault.New(fault.Missing, "current credential metadata is unavailable for this guard")
	}
	metadata, ok := result.credential.value.(C)
	if !ok {
		return *new(C), fault.New(fault.Internal, "invalid current credential metadata")
	}
	return metadata, nil
}

// CurrentProof returns a proof for the model that authenticated guard in this
// scope, retaining the verified credential's access-scope grants. Issuing a new
// token from it therefore inherits, and can only narrow, the request's grants,
// instead of minting an unrestricted proof with NewProof. The provider must be
// the guard's own declaration. The proof carries no issuance check or metadata.
// A scope authenticated by an impersonation credential is refused with
// ImpersonationForbidden.
func CurrentProof[M model.Identifiable, K any](ctx context.Context, provider Provider[M, K], guard Guard[M]) (Proof[M, K], error) {
	if err := provider.ValidateGuard(guard); err != nil {
		return Proof[M, K]{}, err
	}
	result, err := guard.resolve(ctx)
	if err != nil {
		return Proof[M, K]{}, err
	}
	if !result.subject.IsSet() {
		return Proof[M, K]{}, Unauthenticated
	}
	if result.credential != nil && result.credential.impersonated {
		return Proof[M, K]{}, ImpersonationForbidden
	}
	reference, err := provider.Parse(result.identity)
	if err != nil {
		return Proof[M, K]{}, err
	}
	proof, err := NewProof(reference, Authenticated)
	if err != nil {
		return Proof[M, K]{}, err
	}
	proof.grants = result.grants
	return proof, nil
}
