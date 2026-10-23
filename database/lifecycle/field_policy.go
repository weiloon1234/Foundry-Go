package lifecycle

import (
	"database/sql/driver"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/value"
)

// FieldPolicy is immutable codec comparison and snapshot ownership metadata.
// Its zero value is invalid. Example: policy := FieldPolicyOf(codec.Bytes[[]byte]()).
type FieldPolicy[V any] struct {
	bind  func(V) (driver.Value, error)
	clone func(V) V
	equal func(V, V) bool
}

// FieldPolicyOf retains the original codec's binding, equality and ownership.
// It constructs no representation adapter. Example: FieldPolicyOf(codec.String[string]()).
func FieldPolicyOf[V any](c codec.Codec[V]) FieldPolicy[V] {
	policy := FieldPolicy[V]{bind: c.Bind, clone: c.Clone}
	if c.ComparesValues() {
		policy.equal = func(a, b V) bool { same, _ := c.Equal(a, b); return same }
	}
	return policy
}
func cloneOptional[V any](policy *FieldPolicy[V], input value.Optional[V]) value.Optional[V] {
	v, ok := input.Get()
	if !ok {
		return value.Optional[V]{}
	}
	return value.Set(policy.clone(v))
}
