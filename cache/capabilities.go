package cache

import "github.com/weiloon1234/Foundry-Go/fault"

// Requirements declares capabilities an application's cache declarations need.
// Assembly checks them before boot instead of failing at the first request.
type Requirements struct{ Tags, Counters, Entries, Batches, DistributedFills bool }

func (s *Store) Require(required Requirements) error {
	if s == nil || s.backend == nil {
		return fault.New(fault.Invalid, "cache store is not initialized")
	}
	_, tags := s.backend.(TaggedBackend)
	_, counters := s.backend.(CounterBackend)
	_, entries := s.backend.(EntryBackend)
	_, batches := s.backend.(BatchBackend)
	if required.Tags && !tags || required.Counters && !counters || required.Entries && !entries || required.Batches && !batches || required.DistributedFills && s.coordination == nil {
		return fault.New(fault.Invalid, "cache backend does not support required capabilities")
	}
	if tags {
		_, counter := s.backend.(TaggedCounterBackend)
		_, entry := s.backend.(TaggedEntryBackend)
		_, batch := s.backend.(TaggedBatchBackend)
		if required.Counters && !counter || required.Entries && !entry || required.Batches && !batch {
			return fault.New(fault.Invalid, "cache backend does not support required namespace capabilities")
		}
	}
	return nil
}
