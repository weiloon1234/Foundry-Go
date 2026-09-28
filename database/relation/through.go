package relation

// Link keeps each related model beside the concrete pivot model for that edge.
// A target shared by several parents never acquires another parent's pivot data.
type Link[M, P any] struct {
	Model M
	Pivot P
}

// Through is a loaded many-to-many collection with typed pivot data. Distinct
// pivot rows remain distinct links even when they refer to the same target.
type Through[M, P any] struct{ items Many[Link[M, P]] }

// Linked constructs a loaded collection, including a loaded empty collection.
func Linked[M, P any](items []Link[M, P]) Through[M, P] {
	return Through[M, P]{items: Collection(items)}
}
func (r Through[M, P]) IsLoaded() bool { return r.items.IsLoaded() }

// Get returns a copied link collection and whether it was loaded. Model fields
// inside each link retain ordinary Go value-copy semantics.
func (r Through[M, P]) Get() ([]Link[M, P], bool) { return r.items.Get() }
