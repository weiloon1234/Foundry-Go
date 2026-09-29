package relation

import (
	"iter"

	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

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

// Len reports the number of loaded links; it is zero when not loaded.
func (r Through[M, P]) Len() int { return r.items.Len() }

// All yields each loaded link by value without copying the collection.
func (r Through[M, P]) All() iter.Seq2[int, Link[M, P]] { return r.items.All() }

// FoundryLinked adopts a freshly built, unshared link slice without copying.
// Only framework loaders can name the seal; applications use Linked.
func FoundryLinked[M, P any](seal sqlowner.Seal, items []Link[M, P]) Through[M, P] {
	return Through[M, P]{items: FoundryCollection(seal, items)}
}
