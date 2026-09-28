package cache

import (
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// MaxBatchEntries bounds input keys before canonicalization. Config may lower it.
const MaxBatchEntries = 256

// ValidateBatchKeys validates an adapter batch's namespace, order and uniqueness.
// Empty input is valid; the adapter still validates its lifecycle and context.
func ValidateBatchKeys(keys []EntryKey) error {
	if len(keys) > MaxBatchEntries {
		return fault.New(fault.Invalid, "cache batch exceeds key limit")
	}
	for i, key := range keys {
		if err := key.Validate(); err != nil {
			return err
		}
		if key.Namespace() != keys[0].Namespace() {
			return fault.New(fault.Invalid, "cache batch crosses namespaces")
		}
		if i > 0 && strings.Compare(keys[i-1].String(), key.String()) >= 0 {
			return fault.New(fault.Invalid, "cache batch must be canonical and unique")
		}
	}
	return nil
}

// ValidateTaggedBatch requires bounded canonical data addresses and the same exact
// immutable tag stamps on every entry, so one atomic metadata check covers a batch.
func ValidateTaggedBatch(keys []TaggedKey) error {
	if len(keys) > MaxBatchEntries {
		return fault.New(fault.Invalid, "tagged cache batch exceeds key limit")
	}
	for i, key := range keys {
		if err := key.Validate(); err != nil {
			return err
		}
		if key.DataKey().Namespace() != keys[0].DataKey().Namespace() || !slices.Equal(key.data.stamps, keys[0].data.stamps) {
			return fault.New(fault.Invalid, "cache batch requires one namespace and tag snapshot")
		}
		if i > 0 && strings.Compare(keys[i-1].DataKey().String(), key.DataKey().String()) >= 0 {
			return fault.New(fault.Invalid, "tagged cache batch must be canonical and unique")
		}
	}
	return nil
}
