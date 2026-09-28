package relation

import "github.com/weiloon1234/Foundry-Go/value"

// Value is a loaded aggregate slot. V describes SQL nullability independently
// of loaded state: for example Value[value.Nullable[decimal.Decimal]].
type Value[V any] struct{ result value.Optional[V] }

// Computed constructs a loaded aggregate, including zero or a SQL NULL value.
func Computed[V any](v V) Value[V] { return Value[V]{result: value.Set(v)} }
func (v Value[V]) IsLoaded() bool  { return v.result.IsSet() }
func (v Value[V]) Get() (V, bool)  { return v.result.Get() }
