package translations

import "unsafe"

// FieldOf returns the slot's existing declaration for explicit batch reads and matching.
// It preserves registration identity; a zero handle returns an invalid zero field.
// Example: translations.FieldOf(bound.Title).Load(ctx, manager, references).
func FieldOf[M any, K comparable](slot TextHandle[M, K]) Field[M, K] {
	var field Field[M, K]
	if slot.core != nil {
		slot.core.field(unsafe.Pointer(&field))
	}
	return field
}
