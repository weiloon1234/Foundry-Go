package deployment

import (
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
