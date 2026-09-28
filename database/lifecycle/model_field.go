package lifecycle

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// CompareModelField is a generated declaration boundary. One concrete M owns
// both snapshots and the persisted-field getter, and V owns the codec and change
// result. The getter must only extract its persisted field, without I/O or read
// accessors. Generated model comparisons enumerate these declarations.
func CompareModelField[M, V any](c codec.Codec[V], before, after value.Optional[M], assigned bool, get func(M) V) (FieldChange[V], error) {
	if get == nil {
		return FieldChange[V]{}, fault.New(fault.Invalid, "model field comparison requires a getter")
	}
	var left, right value.Optional[V]
	if item, present := before.Get(); present {
		left = value.Set(get(item))
	}
	if item, present := after.Get(); present {
		right = value.Set(get(item))
	}
	return CompareField(c, left, right, assigned)
}
