package codec

import "github.com/weiloon1234/Foundry-Go/value"

// Clone copies a persistence value without binding, decoding or validation.
// Built-in binary codecs copy their buffers; immutable values are returned by
// value. Custom codecs retain ordinary Go value semantics: their owners must
// treat any referenced data as read-only. Nullable and Validated preserve this
// ownership policy.
func (c Codec[T]) Clone(v T) T {
	if c.clone != nil {
		return c.clone(v)
	}
	return v
}

// CloneOptional preserves omission while applying the codec's ownership policy.
func (c Codec[T]) CloneOptional(v value.Optional[T]) value.Optional[T] {
	item, present := v.Get()
	if !present {
		return value.Optional[T]{}
	}
	return value.Set(c.Clone(item))
}

func (c Codec[T]) withClone(clone func(T) T) Codec[T] { c.clone = clone; return c }
