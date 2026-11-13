package application

import (
	"maps"
	"path/filepath"

	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/storage/local"
)

// Apply deployment sublimits only to the owners explicitly selected by their
// configuration. Local staging remains inside the selected managed store root.
func prepareUploadSettings(s *Settings) error {
	uploads := s.HTTP.Uploads
	if !uploads.Enabled {
		return nil
	}
	s.Features.Attachments.Config.MaxLargeActive = uploads.AttachmentMaxActive
	if err := s.Features.Attachments.Config.Validate(); err != nil {
		return err
	}
	s.Services.Storage.Disks = maps.Clone(s.Services.Storage.Disks)
	for name, disk := range s.Services.Storage.Disks {
		if disk.Driver != infrastructure.LocalDisk || filepath.Clean(disk.Local.Root) != filepath.Clean(uploads.Staging.Directory) {
			continue
		}
		disk.Config.Timeout = uploads.StorageTimeout
		disk.Local.Staging = local.StagingLimits{MaxActive: uploads.Staging.MaxActive, MaxBytes: uploads.Staging.MaxBytes, SmallObjectBytes: attachments.MaxBufferedBytes}
		s.Services.Storage.Disks[name] = disk
	}
	return nil
}
