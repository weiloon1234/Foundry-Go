package jobs

import (
	"cmp"
	"slices"
)

// Description contains declaration metadata only, never queued payloads,
// handler callbacks or backend state. Policy slices are independently owned.
type Description struct {
	Name    Name    `json:"name"`
	Version Version `json:"version"`
	Payload string  `json:"payload"`
	Policy  Policy  `json:"policy"`
}

func (r *Registry) Describe() []Description {
	if r == nil {
		return nil
	}
	result := make([]Description, 0, len(r.entries))
	for _, entry := range r.entries {
		result = append(result, Description{entry.key.name, entry.key.version, entry.typ.String(), entry.policy.snapshot()})
	}
	slices.SortFunc(result, func(a, b Description) int {
		if order := cmp.Compare(a.Name, b.Name); order != 0 {
			return order
		}
		return cmp.Compare(a.Version, b.Version)
	})
	return result
}

// Describe inspects the actual bound registry without contacting the queue.
// Call after application assembly, when contribution binding is complete.
func (d *Dispatcher) Describe() []Description {
	if d == nil {
		return nil
	}
	return d.registry.Describe()
}
