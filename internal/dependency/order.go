// Package dependency owns deterministic dependency traversal for explicit
// framework registries. Callers own identifier validation and input ordering.
package dependency

import "fmt"

type Kind uint8

const (
	Missing Kind = iota + 1
	Cycle
)

type Failure[K comparable] struct {
	Kind Kind
	Key  K
}

func (f *Failure[K]) Error() string {
	if f.Kind == Cycle {
		return fmt.Sprintf("dependency cycle at %v", f.Key)
	}
	return fmt.Sprintf("missing dependency %v", f.Key)
}

// Order visits roots and dependency lists in supplied order, emitting each node
// once after its prerequisites. It performs no registration or runtime work.
func Order[K comparable](roots []K, dependencies func(K) ([]K, bool)) ([]K, *Failure[K]) {
	state := make(map[K]uint8, len(roots))
	ordered := make([]K, 0, len(roots))
	var visit func(K) *Failure[K]
	visit = func(key K) *Failure[K] {
		if state[key] == 1 {
			return &Failure[K]{Kind: Cycle, Key: key}
		}
		if state[key] == 2 {
			return nil
		}
		requires, exists := dependencies(key)
		if !exists {
			return &Failure[K]{Kind: Missing, Key: key}
		}
		state[key] = 1
		for _, required := range requires {
			if failure := visit(required); failure != nil {
				return failure
			}
		}
		state[key] = 2
		ordered = append(ordered, key)
		return nil
	}
	for _, root := range roots {
		if failure := visit(root); failure != nil {
			return nil, failure
		}
	}
	return ordered, nil
}
