package generate

import (
	"fmt"
	"path/filepath"
)

const distributionOwnershipProof = "distribution-after.json"

// A v6 journal can touch ordinary asset/scaffold filenames, so its old/new
// ownership manifests must prove every replacement before recovery mutates a
// target. Older journal versions retain their original narrow filename rules.
func validateRecoveryDistribution(root string, log journal) error {
	if log.Version != 6 {
		return nil
	}
	policy := distributionPolicy(*log.Distribution)
	entry := log.Files[manifestName]
	before := manifest{Version: policy.version, Files: map[string]string{}, Distribution: log.Distribution}
	if entry.Before.Exists {
		backup, err := readWithin(root, filepath.Join(lockName, stageName("old", manifestName)))
		if err != nil {
			return err
		}
		if !entry.Before.matches(backup) {
			return fmt.Errorf("plugin recovery ownership backup is invalid")
		}
		before, err = decodeManifestPolicy(backup.data, policy)
		if err != nil {
			return err
		}
	}
	// The immutable proof is never renamed into place. A second crash after
	// restoring the old manifest therefore still has both ownership states.
	data, err := readWithin(root, filepath.Join(lockName, distributionOwnershipProof))
	if err != nil {
		return err
	}
	if !entry.After.matches(data) {
		return fmt.Errorf("plugin recovery new ownership manifest is invalid")
	}
	after, err := decodeManifestPolicy(data.data, policy)
	if err != nil {
		return err
	}
	if *after.Distribution != *log.Distribution {
		return fmt.Errorf("plugin recovery release differs from its ownership manifest")
	}
	order, err := after.Distribution.Release.Compare(before.Distribution.Release)
	if err != nil || order < 0 {
		return fmt.Errorf("plugin recovery cannot regress an installed release")
	}
	for _, metadata := range []manifest{before, after} {
		paths := make(map[string][]byte, len(metadata.Files))
		for name := range metadata.Files {
			paths[name] = nil
		}
		if _, err := SnapshotDistribution(paths); err != nil {
			return err
		}
	}
	for name, change := range log.Files {
		if name == manifestName {
			continue
		}
		if change.Before.Exists && before.Files[name] != change.Before.Hash {
			return fmt.Errorf("plugin recovery cannot replace an unowned file: %s", name)
		}
		next, exists := after.Files[name]
		if change.After.Exists != exists || (exists && next != change.After.Hash) {
			return fmt.Errorf("plugin recovery target differs from its new ownership: %s", name)
		}
	}
	paths := make(map[string]bool)
	for name := range before.Files {
		paths[name] = true
	}
	for name := range after.Files {
		paths[name] = true
	}
	for name := range paths {
		if before.Files[name] != after.Files[name] {
			if _, exists := log.Files[name]; !exists {
				return fmt.Errorf("plugin recovery omits an ownership change: %s", name)
			}
		}
	}
	return nil
}
