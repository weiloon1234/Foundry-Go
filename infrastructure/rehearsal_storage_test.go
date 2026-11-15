package infrastructure_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/cloud/credentials"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/internal/deployrehearsal"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/storage"
)

func TestRehearsalGrantAppliesOnlyToConfiguredDisks(t *testing.T) {
	for _, tc := range []struct {
		name     string
		driver   infrastructure.DiskDriver
		capDelta int64
		invalid  bool
	}{
		{name: "absent"},
		{name: "local", driver: infrastructure.LocalDisk, invalid: true},
		{name: "s3", driver: infrastructure.S3Disk, invalid: true},
		{name: "r2", driver: infrastructure.R2Disk, invalid: true},
		{name: "compatible", driver: infrastructure.CompatibleDisk},
		{name: "mismatched cap", driver: infrastructure.CompatibleDisk, capDelta: 1, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := infrastructure.DefaultSettings()
			disk := infrastructure.DefaultDiskSettings()
			disk.Driver = tc.driver
			disk.Local.Root = t.TempDir()
			disk.Cloud.Bucket, disk.Cloud.Region, disk.Cloud.Endpoint = "fixture-bucket", "us-east-1", "https://unused.example.test"
			disk.Cloud.Credentials = "rehearsal"
			disk.Cloud.Compatible.ConditionalCreate = true
			disk.Cloud.Compatible.SingleRequestConditions = true
			if tc.driver != "" {
				s.Storage.Disks = infrastructure.Disks{"default": disk}
				s.Credentials = infrastructure.CredentialSources{"rehearsal": {Mode: credentials.Static, AccessKey: secret.New("test"), SecretKey: secret.New("test")}}
			}
			grant := deployrehearsal.New(map[storage.DiskID]int64{
				"default": disk.Cloud.PartBytes + tc.capDelta,
				"absent":  disk.Cloud.PartBytes,
			})
			plan, err := infrastructure.Configure(s, infrastructure.WithRehearsal(grant))
			if tc.invalid {
				if !errors.Is(err, storage.Invalid) {
					t.Fatal("invalid granted disk accepted", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			app := built(t, plan)
			if err := app.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			if len(services(t, app).Storage.Disks()) != len(s.Storage.Disks) {
				t.Fatal("grant added an unconfigured disk")
			}
			if tc.driver != "" {
				runtimeDisk, err := services(t, app).Storage.Disk("default")
				if err != nil || runtimeDisk.Capabilities().ConditionalWriteMode != storage.RehearsalRuntime {
					t.Fatal("configured disk lost its grant", err)
				}
			}
			if len(grant.Disks()) != 2 {
				t.Fatal("runtime subset mutated the grant")
			}
		})
	}
}

func TestOperatorConfigurationCannotEnableRehearsal(t *testing.T) {
	const declaration = `{"uploads":{"driver":"s3_compatible","cloud":{"conditional_write_mode":"rehearsal_runtime","compatible":{"conditional_create":true,"single_request_conditions":true}}}}`
	for _, id := range []string{"", strings.Repeat("a", 32)} {
		t.Setenv("FOUNDRY_REHEARSAL_RUN_ID", id)
		var disks infrastructure.Disks
		if err := disks.UnmarshalText([]byte(declaration)); err == nil || len(disks) != 0 {
			t.Fatal("retired operator key accepted", err)
		}
	}
}
