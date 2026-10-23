package ormcore

import (
	"reflect"

	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// referenceSet bounds both visited inputs and retained unique references. Stable
// insertion order keeps validation deterministic; caller slices are never reused.
type referenceSet struct {
	values  []Reference
	seen    map[Reference]struct{}
	visited int
	err     error
}

func (s *referenceSet) add(refs []Reference) {
	for _, r := range refs {
		if s.err != nil {
			return
		}
		s.visited++
		if s.visited > querycore.MaxExpressionNodes {
			s.err = Invalid("expression references exceed their resource bound")
			return
		}
		if r.Owner != nil && !reflect.ValueOf(r.Owner).Comparable() {
			s.err = Invalid("expression owner must be a declaration identity")
			return
		}
		if s.seen == nil {
			s.seen = make(map[Reference]struct{})
		}
		if _, ok := s.seen[r]; ok {
			continue
		}
		s.seen[r] = struct{}{}
		s.values = append(s.values, r)
	}
}

// MergeReferences snapshots a bounded set of declaration and scope identities.
func MergeReferences(groups ...[]Reference) ([]Reference, error) {
	var set referenceSet
	for _, group := range groups {
		set.add(group)
	}
	return set.values, set.err
}
