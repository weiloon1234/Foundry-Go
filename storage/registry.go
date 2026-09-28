package storage

import (
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/namedservice"
)

const MaxDisks = namedservice.MaxEntries

// Declaration is a reusable typed disk descriptor. IDs are explicit, not guessed
// from default configuration. Separate domain concerns should declare disks once.
type Declaration struct{ id DiskID }

func DefineDisk(id DiskID) Declaration { return Declaration{id: id} }
func (d Declaration) ID() DiskID       { return d.id }
func (d Declaration) Validate() error  { return d.id.Validate() }
func (d Declaration) Bind(backend Backend, config Config) (*Disk, error) {
	return NewDisk(d.id, backend, config)
}
func (d Declaration) Resolve(registry *Registry) (*Disk, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return registry.Disk(d.id)
}

// Registry is immutable and borrows its disks. The lifecycle owner closes each
// disk before its backend. Registry construction and resolution perform no I/O.
type Registry struct {
	disks    map[DiskID]*Disk
	names    []DiskID
	selected DiskID
}

func NewRegistry(disks ...*Disk) (*Registry, error) {
	if len(disks) > MaxDisks {
		return nil, Failure(LimitExceeded, OpenOperation, NotApplicable, nil)
	}
	registry := &Registry{disks: make(map[DiskID]*Disk, len(disks))}
	for _, disk := range disks {
		if err := disk.Validate(); err != nil {
			return nil, err
		}
		if _, duplicate := registry.disks[disk.ID()]; duplicate {
			return nil, fault.New(fault.Duplicate, "storage disk is already registered")
		}
		registry.disks[disk.ID()] = disk
		registry.names = append(registry.names, disk.ID())
	}
	slices.Sort(registry.names)
	return registry, nil
}
func (r *Registry) Disk(id DiskID) (*Disk, error) {
	if r == nil || r.disks == nil {
		return nil, Failure(Invalid, OpenOperation, NotApplicable, nil)
	}
	if err := id.Validate(); err != nil {
		return nil, err
	}
	disk, exists := r.disks[id]
	if !exists {
		return nil, fault.New(fault.Missing, "storage disk is not registered")
	}
	return disk, nil
}
func (r *Registry) Disks() []DiskID {
	if r == nil {
		return nil
	}
	return slices.Clone(r.names)
}

// NewDefaultRegistry selects an existing disk. Default is an alias, never a
// separately opened backend. NewRegistry remains available for explicit-only use.
func NewDefaultRegistry(selected DiskID, disks ...*Disk) (*Registry, error) {
	r, err := NewRegistry(disks...)
	if err != nil {
		return nil, err
	}
	if _, err = r.Disk(selected); err != nil {
		return nil, err
	}
	r.selected = selected
	return r, nil
}
func (r *Registry) Default() (*Disk, error) {
	if r == nil || r.selected == "" {
		return nil, fault.New(fault.Missing, "default storage disk is not configured")
	}
	return r.Disk(r.selected)
}
func (r *Registry) DefaultName() DiskID {
	if r == nil {
		return ""
	}
	return r.selected
}
