package application

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/storage"
)

func registerRehearsalStorage(builder *foundation.Builder, settings Settings) {
	attachmentsEnabled := settings.Features.Attachments.Enabled
	requires := []foundation.ProviderID{Provider}
	if attachmentsEnabled {
		requires = append(requires, AttachmentProvider)
	}
	builder.Register(foundation.Module{Name: "foundry.application.rehearsal_storage", Requires: requires,
		OnBoot: func(ctx context.Context, r *foundation.Runtime) error {
			services, err := FromResolver(r.Services())
			if err != nil {
				return err
			}
			var policies map[storage.DiskID][]storage.WritePolicyMaximum
			if attachmentsEnabled {
				manager, err := services.Attachments()
				if err != nil {
					return err
				}
				policies = manager.RehearsalPolicies()
			}
			for _, name := range services.Storage.Disks() {
				disk, err := services.Storage.Disk(name)
				if err != nil {
					return err
				}
				disk.LogRehearsal(ctx, policies[name])
			}
			return nil
		}})
}
