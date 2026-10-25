package attachments

import "unsafe"

// OneCollection returns the single-slot declaration for explicit collection operations.
// It preserves registration identity and policy; a zero handle returns an invalid
// zero collection. Example: attachments.OneCollection(bound.Logo).List(ctx,manager,reference).
func OneCollection[M any, K comparable](slot OneHandle[M, K]) Collection[M, K] {
	var collection Collection[M, K]
	if slot.core != nil {
		slot.core.collection(unsafe.Pointer(&collection))
	}
	return collection
}

// ManyCollection returns the multiple-slot declaration for explicit batch reads and matching.
// It preserves registration identity and policy; a zero handle returns an invalid
// zero collection. Example: attachments.ManyCollection(bound.Gallery).List(ctx,manager,reference).
func ManyCollection[M any, K comparable](slot ManyHandle[M, K]) Collection[M, K] {
	var collection Collection[M, K]
	if slot.core != nil {
		slot.core.collection(unsafe.Pointer(&collection))
	}
	return collection
}
