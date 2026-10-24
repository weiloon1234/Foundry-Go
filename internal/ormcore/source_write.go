package ormcore

import (
	"reflect"

	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// ModelMapping retains both declaration ownership and the exact stored value type.
type ModelMapping struct {
	Destination Reference
	Field       querycore.FieldRef
	Stored      reflect.Type
	Value       Expression
	NullableKey bool
	Err         error
}
