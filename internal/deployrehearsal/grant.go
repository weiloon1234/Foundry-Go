// Package deployrehearsal seals grants issued by the validated deployment reader.
// Application configuration cannot construct a nonzero grant.
package deployrehearsal

import "maps"

// Grant is an immutable per-disk cap snapshot. Its zero value grants nothing.
type Grant[K comparable] struct{ caps *map[K]int64 }

// New is used only after the deploy descriptor and process identity are validated.
func New[K comparable](caps map[K]int64) Grant[K] {
	owned := maps.Clone(caps)
	return Grant[K]{caps: &owned}
}

func (g Grant[K]) Cap(name K) (int64, bool) {
	if g.caps == nil {
		return 0, false
	}
	cap, ok := (*g.caps)[name]
	return cap, ok
}

func (g Grant[K]) Disks() map[K]int64 {
	if g.caps == nil {
		return nil
	}
	return maps.Clone(*g.caps)
}
