package lease

import (
	"crypto/rand"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

const OwnerBytes = 32

// Owner is an opaque, random adapter token. It is never exposed by application guards.
type Owner struct{ value [OwnerBytes]byte }

// NewOwner creates an owner token using cryptographic randomness.
func NewOwner() (Owner, error) {
	var o Owner
	if _, err := rand.Read(o.value[:]); err != nil {
		return Owner{}, safeerror.WrapFailure(fault.Internal, "lease entropy failed", err)
	}
	return o, o.Validate()
}

// ParseOwner copies and validates an owner token of exactly OwnerBytes bytes.
func ParseOwner(data []byte) (Owner, error) {
	if len(data) != OwnerBytes {
		return Owner{}, safeerror.NewFault(fault.Invalid, "invalid lease owner size")
	}
	var o Owner
	copy(o.value[:], data)
	return o, o.Validate()
}

// Validate rejects the all-zero owner token with fault.Invalid.
func (o Owner) Validate() error {
	if o.value == [OwnerBytes]byte{} {
		return safeerror.NewFault(fault.Invalid, "lease owner is not initialized")
	}
	return nil
}

// Bytes returns an owned copy of the secret owner token.
func (o Owner) Bytes() []byte { return slices.Clone(o.value[:]) }

// String returns a redacted description of the owner.
func (o Owner) String() string { return "[lease owner]" }

// GoString returns a redacted description of the owner.
func (o Owner) GoString() string { return o.String() }
