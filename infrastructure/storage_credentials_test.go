package infrastructure_test

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/cloud/credentials"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/storage"
)

// A compatible (MinIO/Spaces/B2) endpoint must never receive the host's AWS
// machine credentials from the SDK default chain.
func TestCompatibleDisksRequireExplicitStaticCredentials(t *testing.T) {
	configured := func(mode credentials.Mode, source credentials.Name) infrastructure.Settings {
		s := memorySettings()
		disk := infrastructure.DefaultDiskSettings()
		disk.Driver = infrastructure.CompatibleDisk
		disk.Cloud.Bucket, disk.Cloud.Region, disk.Cloud.Endpoint = "fixture-bucket", "us-east-1", "https://minio.example.test"
		disk.Cloud.Credentials = source
		s.Storage.Default = "files"
		s.Storage.Disks = infrastructure.Disks{storage.DiskID("files"): disk}
		s.Credentials = infrastructure.CredentialSources{"minio": {Mode: mode, AccessKey: secret.New("fixture-access"), SecretKey: secret.New("fixture-secret")}}
		if mode == credentials.Chain {
			s.Credentials["minio"] = credentials.Settings{Mode: credentials.Chain}
		}
		return s
	}
	for name, settings := range map[string]infrastructure.Settings{
		"no credentials":    configured(credentials.Static, ""),
		"chain credentials": configured(credentials.Chain, "minio"),
	} {
		if _, err := infrastructure.Configure(settings); err == nil {
			t.Fatal("compatible disk accepted implicit machine credentials:", name)
		}
	}
	if _, err := infrastructure.Configure(configured(credentials.Static, "minio")); err != nil {
		t.Fatal("explicit static credentials rejected", err)
	}
}
