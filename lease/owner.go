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

func NewOwner() (Owner, error) {
	var o Owner
	if _, err := rand.Read(o.value[:]); err != nil {
		return Owner{}, safeerror.WrapFailure(fault.Internal, "lease entropy failed", err)
	}
	return o, o.Validate()
}
func ParseOwner(data []byte) (Owner, error) {
	if len(data) != OwnerBytes {
		return Owner{}, safeerror.NewFault(fault.Invalid, "invalid lease owner size")
	}
	var o Owner
	copy(o.value[:], data)
	return o, o.Validate()
}
func (o Owner) Validate() error {
	if o.value == [OwnerBytes]byte{} {
		return safeerror.NewFault(fault.Invalid, "lease owner is not initialized")
	}
	return nil
}
func (o Owner) Bytes() []byte    { return slices.Clone(o.value[:]) }
func (o Owner) String() string   { return "[lease owner]" }
func (o Owner) GoString() string { return o.String() }
