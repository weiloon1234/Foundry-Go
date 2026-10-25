package metadata

import "unsafe"

// KeyOf returns the slot's existing key for explicit batch reads and matching.
// It preserves registration identity and the concrete value type; a zero handle
// returns an invalid zero key. Example: metadata.KeyOf(bound.SEO).Get(ctx,manager,reference).
func KeyOf[M any, K comparable, V any](slot ValueHandle[M, K, V]) Key[M, K, V] {
	var key Key[M, K, V]
	if slot.core != nil {
		slot.core.key(unsafe.Pointer(&key))
	}
	return key
}
