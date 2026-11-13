package application

import (
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
)

func TestUploadSettingsReachConfiguredOwnersWithoutMutatingInputs(t *testing.T) {
	original := DefaultSettings()
	original.HTTP.Uploads = http.UploadSettings{Enabled: true, AttachmentMaxActive: 2, StorageTimeout: 3 * time.Minute, Staging: http.UploadPoolConfig{Directory: t.TempDir(), MaxActive: 2, MaxBytes: 2 << 30}}
	selected := infrastructure.DefaultDiskSettings()
	selected.Local.Root = original.HTTP.Uploads.Staging.Directory
	other := infrastructure.DefaultDiskSettings()
	other.Local.Root = t.TempDir()
	original.Services.Storage.Disks = infrastructure.Disks{"selected": selected, "other": other}
	configured := original
	if err := prepareUploadSettings(&configured); err != nil {
		t.Fatal(err)
	}
	if configured.Features.Attachments.Config.MaxLargeActive != 2 {
		t.Fatal("attachment sublimit not applied")
	}
	actual := configured.Services.Storage.Disks["selected"]
	if actual.Local.Staging.MaxActive != 2 || actual.Local.Staging.MaxBytes != 2<<30 || actual.Local.Staging.SmallObjectBytes != attachments.MaxBufferedBytes || actual.Config.Timeout != 3*time.Minute {
		t.Fatal("local owner did not receive deployment limits")
	}
	if configured.Services.Storage.Disks["other"] != other || original.Services.Storage.Disks["selected"] != selected || original.Features.Attachments.Config.MaxLargeActive != 0 {
		t.Fatal("unselected or caller-owned configuration changed")
	}
}

func TestUploadSettingsRejectStarvationAndPreserveDisabledDefaults(t *testing.T) {
	settings := DefaultSettings()
	settings.HTTP.Uploads.AttachmentMaxActive = settings.Features.Attachments.Config.MaxActive
	if err := prepareUploadSettings(&settings); err != nil {
		t.Fatal("disabled upload settings changed defaults", err)
	}
	settings.HTTP.Uploads.Enabled = true
	if err := prepareUploadSettings(&settings); err == nil {
		t.Fatal("large uploads can exhaust all manager capacity")
	}
}
