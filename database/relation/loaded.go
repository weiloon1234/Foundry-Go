// Package relation provides explicit loaded states for typed model relations.
// Reading a relation value never performs database I/O.
package relation

import (
	"slices"

	"github.com/weiloon1234/Foundry-Go/value"
)

// One distinguishes not loaded, loaded empty and loaded with a model. Its private
// pointer permits self-relations without an infinitely sized Go struct.
type One[M any] struct {
	model  *M
	loaded bool
}

// Single constructs a loaded singular relation, including an absent model.
func Single[M any](model value.Optional[M]) One[M] {
	result := One[M]{loaded: true}
	if m, present := model.Get(); present {
		result.model = &m
	}
	return result
}

func (r One[M]) IsLoaded() bool { return r.loaded }

// Get returns the optional related model and whether the relation was loaded.
// A loaded but absent model differs from a relation that has not been loaded.
func (r One[M]) Get() (value.Optional[M], bool) {
	if r.model == nil {
		return value.Optional[M]{}, r.loaded
	}
	return value.Set(*r.model), r.loaded
}

// Many distinguishes not loaded from a loaded collection, including an empty
// collection. Its private slice is copied at construction and retrieval.
type Many[M any] struct {
	items  []M
	loaded bool
}

func Collection[M any](items []M) Many[M] { return Many[M]{items: slices.Clone(items), loaded: true} }
func (r Many[M]) IsLoaded() bool          { return r.loaded }
func (r Many[M]) Get() ([]M, bool)        { return slices.Clone(r.items), r.loaded }
