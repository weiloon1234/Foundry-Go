package deployment

import (
	"encoding/json"
	"os"
	"regexp"
	"syscall"

	"github.com/weiloon1234/Foundry-Go/internal/deployrehearsal"
	"github.com/weiloon1234/Foundry-Go/storage"
)

const RehearsalRunEnvironment = "FOUNDRY_REHEARSAL_RUN_ID"

var rehearsalRunID = regexp.MustCompile(`^[a-f0-9]{32}$`)

type rehearsalDiskWire struct {
	Disk            storage.DiskID `json:"disk"`
	RuntimeCapBytes int64          `json:"runtime_cap_bytes"`
}

type rehearsalWire struct {
	RunID string              `json:"run_id"`
	Disks []rehearsalDiskWire `json:"disks"`
}

// Rehearsal returns the sealed storage grant captured when this descriptor was
// read. Absent blocks and mismatched process identities grant nothing.
func (b Bindings) Rehearsal() storage.Rehearsal { return b.rehearsal }

// LoadRehearsal reads only the optional rehearsal grant from the selected runtime
// descriptor. The boolean reports block presence. No descriptor or no block
// grants nothing and imposes no descriptor schema requirement on application
// assembly. A present block requires the full protected schema-2 descriptor.
func LoadRehearsal() (storage.Rehearsal, bool, error) {
	return loadRehearsalOwned(rootOwnedDescriptor)
}

func loadRehearsalOwned(owned func(os.FileInfo) bool) (storage.Rehearsal, bool, error) {
	path, exists := os.LookupEnv(BindingsEnvironment)
	if !exists {
		return storage.Rehearsal{}, false, nil
	}
	data, info, err := readRuntimeDescriptor(path)
	if err != nil {
		return storage.Rehearsal{}, false, err
	}
	var envelope struct {
		Rehearsal json.RawMessage `json:"rehearsal"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return storage.Rehearsal{}, false, invalidBindings()
	}
	if len(envelope.Rehearsal) == 0 {
		return storage.Rehearsal{}, false, nil
	}
	wire, err := decodeFeatureDescriptor(data, info, owned)
	if err != nil {
		return storage.Rehearsal{}, true, err
	}
	if wire.Rehearsal == nil {
		return storage.Rehearsal{}, true, invalidBindings()
	}
	grant, err := wire.Rehearsal.grant(os.Getenv(RehearsalRunEnvironment))
	return grant, true, err
}

func (w *rehearsalWire) grant(runID string) (storage.Rehearsal, error) {
	if w == nil {
		return storage.Rehearsal{}, nil
	}
	if !rehearsalRunID.MatchString(w.RunID) || len(w.Disks) == 0 || len(w.Disks) > storage.MaxDisks {
		return storage.Rehearsal{}, invalidBindings()
	}
	caps := make(map[storage.DiskID]int64, len(w.Disks))
	for _, disk := range w.Disks {
		if disk.Disk.Validate() != nil || disk.RuntimeCapBytes <= 0 || disk.RuntimeCapBytes > 1<<50 || caps[disk.Disk] != 0 {
			return storage.Rehearsal{}, invalidBindings()
		}
		caps[disk.Disk] = disk.RuntimeCapBytes
	}
	if runID != w.RunID {
		return storage.Rehearsal{}, nil
	}
	return deployrehearsal.New(caps), nil
}

func rootOwnedDescriptor(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == 0 && info.Mode().IsRegular() && info.Mode().Perm()&0022 == 0
}
