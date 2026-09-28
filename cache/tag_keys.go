package cache

import (
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// ValidateTagKeys validates the bounded, canonical address sequence accepted by
// TaggedBackend metadata operations. Adapter implementations share this contract;
// their lifecycle and context checks remain adapter-owned. Input is not mutated.
func ValidateTagKeys(keys []EntryKey) error {
	if len(keys) == 0 || len(keys) > MaxTags+1 {
		return fault.New(fault.Invalid, "invalid cache tag count")
	}
	applicationTags := 0
	for i, key := range keys {
		if !key.namespaceTag {
			applicationTags++
		}
		if err := key.Validate(); err != nil {
			return err
		}
		if key.Namespace() != keys[0].Namespace() {
			return fault.New(fault.Invalid, "cache tags cross namespaces")
		}
		if i > 0 && strings.Compare(keys[i-1].String(), key.String()) >= 0 {
			return fault.New(fault.Invalid, "cache tags must be canonical and unique")
		}
	}
	if applicationTags > MaxTags {
		return fault.New(fault.Invalid, "invalid cache tag count")
	}
	return nil
}
