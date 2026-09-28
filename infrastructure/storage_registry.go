package infrastructure

import (
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/storage"
)

const storageRegistryProvider foundation.ProviderID = "foundry.infrastructure.storage"

var storageRegistryKey = foundation.NewKey[*storage.Registry](string(storageRegistryProvider))

func (p *Plan) storageRegistry() {
	s := p.settings.Storage
	if len(s.Disks) == 0 {
		return
	}
	requires := make([]foundation.ProviderID, 0, len(s.Disks))
	for _, name := range keys(s.Disks) {
		requires = append(requires, DiskProvider(name))
	}
	p.providers = append(p.providers, foundation.Module{Name: storageRegistryProvider, Requires: requires, OnRegister: func(r *foundation.Registrar) error {
		return foundation.Factory(r, storageRegistryKey, func(r foundation.Resolver) (*storage.Registry, error) {
			entries := make([]*storage.Disk, 0, len(s.Disks))
			for _, name := range keys(s.Disks) {
				disk, err := foundation.Resolve(r, DiskKey(name))
				if err != nil {
					return nil, err
				}
				entries = append(entries, disk)
			}
			return storage.NewDefaultRegistry(s.Default, entries...)
		})
	}})
}
