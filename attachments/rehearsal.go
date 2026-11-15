package attachments

import (
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/storage"
)

// RehearsalPolicies returns owned declaration maxima for reduced-fidelity disks.
// These are policy names and limits, never attachment or object identities.
func (m *Manager) RehearsalPolicies() map[storage.DiskID][]storage.WritePolicyMaximum {
	result := make(map[storage.DiskID][]storage.WritePolicyMaximum)
	for _, collection := range m.collections {
		disk, err := collection.policy.Disk.Resolve(m.disks)
		if err != nil || disk.Capabilities().ConditionalWriteMode != storage.RehearsalRuntime {
			continue
		}
		result[disk.ID()] = append(result[disk.ID()], storage.WritePolicyMaximum{
			Name: string(collection.owner) + "." + string(collection.name), MaxBytes: collection.policy.MaxStoredBytes,
		})
	}
	for _, policies := range result {
		slices.SortFunc(policies, func(a, b storage.WritePolicyMaximum) int { return strings.Compare(a.Name, b.Name) })
	}
	return result
}
