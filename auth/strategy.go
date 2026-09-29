package auth

import (
	"context"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Assurance is set by a trusted strategy after credential verification.
// PendingMFA can never enter an ordinary authenticated handler or policy.
type Assurance uint8

const (
	PendingMFA Assurance = iota + 1
	Authenticated
)

func (a Assurance) valid() bool { return a == PendingMFA || a == Authenticated }

// Validate checks a restored credential's assurance before it can be used.
func (a Assurance) Validate() error {
	if !a.valid() {
		return fault.New(fault.Invalid, "invalid authentication assurance")
	}
	return nil
}

// Proof is a verified strategy result retaining model/key ownership. It freezes
// stored identity metadata and contains no credential or authoritative roles.
// Only trusted credential adapters should create proofs; construction itself
// does not verify a password, signature, expiry, revocation or database record.
type Proof[M, K any] struct {
	_          [0]*M
	_          [0]*K
	identity   model.Identity
	assurance  Assurance
	grants     *accessGrant
	issuance   *issuanceCheck
	credential *attachedCredential
}

func (Proof[M, K]) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("authentication proof")) }
func NewProof[M, K any](reference model.Reference[M, K], assurance Assurance) (Proof[M, K], error) {
	if !assurance.valid() {
		return Proof[M, K]{}, fault.New(fault.Invalid, "invalid authentication assurance")
	}
	identity, err := reference.Identity()
	if err != nil {
		return Proof[M, K]{}, err
	}
	return Proof[M, K]{identity: identity, assurance: assurance}, nil
}
func (p Proof[M, K]) Identity() model.Identity { return p.identity }
func (p Proof[M, K]) Assurance() Assurance     { return p.assurance }

// Strategy verifies one explicitly selected credential source. Each guard owns
// its strategy invocation; no implicit fallback from an invalid credential to
// another strategy occurs. Missing inputs do not call Verify. Omitted results
// for present inputs mean invalid credentials, not anonymous authentication.
type Strategy[M, K any] struct {
	source CredentialName
	verify func(context.Context, secret.String) (value.Optional[Proof[M, K]], error)
}

func DefineStrategy[M, K any](source CredentialName, verify func(context.Context, secret.String) (value.Optional[Proof[M, K]], error)) Strategy[M, K] {
	return Strategy[M, K]{source: source, verify: verify}
}
func (s Strategy[M, K]) Validate() error {
	if !identifier.Semantic(string(s.source)) || s.verify == nil {
		return fault.New(fault.Invalid, "authentication strategy requires a source and verifier")
	}
	return nil
}
func (s Strategy[M, K]) Source() CredentialName { return s.source }
