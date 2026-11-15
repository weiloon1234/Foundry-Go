package application_test

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/cloud/credentials"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestRehearsalApplicationStartupNoticeAndRuntimeRefusal(t *testing.T) {
	t.Setenv("FOUNDRY_REHEARSAL_RUN_ID", strings.Repeat("a", 32))
	s := settings()
	s.HTTP.Enabled = false
	disk := infrastructure.DefaultDiskSettings()
	disk.Driver = infrastructure.CompatibleDisk
	disk.Cloud.Bucket, disk.Cloud.Region, disk.Cloud.Endpoint = "fixture-bucket", "us-east-1", "https://unused.example.test"
	disk.Cloud.Credentials = "rehearsal"
	disk.Cloud.Compatible.ConditionalCreate = true
	disk.Cloud.Compatible.SingleRequestConditions = true
	disk.Cloud.ConditionalWriteMode = storage.RehearsalRuntime
	s.Services.Credentials = infrastructure.CredentialSources{"rehearsal": {Mode: credentials.Static, AccessKey: secret.New("private-access"), SecretKey: secret.New("private-secret")}}
	s.Services.Storage.Disks = infrastructure.Disks{"default": disk}
	log, err := os.CreateTemp(t.TempDir(), "startup")
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	previous := os.Stderr
	os.Stderr = log
	defer func() { os.Stderr = previous }()
	app, err := application.New(s, quiet()).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer app.Shutdown(t.Context())
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	services, err := application.FromResolver(app.Services())
	if err != nil {
		t.Fatal(err)
	}
	runtimeDisk, err := services.Storage.Disk("default")
	if err != nil {
		t.Fatal(err)
	}
	// A second report attempt must not duplicate the startup notice.
	runtimeDisk.LogRehearsal(t.Context(), nil)
	key, _ := storage.ParseKey("private-object")
	_, err = runtimeDisk.Put(t.Context(), key, strings.NewReader("private-contents"), storage.PutOptions{Condition: storage.IfAbsent(), Size: value.Set(disk.Cloud.PartBytes + 1)})
	if !errors.Is(err, storage.RehearsalLimitExceeded) {
		t.Fatal(err)
	}
	data, err := os.ReadFile(log.Name())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "private-") {
		t.Fatal("rehearsal logs exposed private data")
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("wanted startup and refusal records, got %d", len(lines))
	}
	for i, line := range lines {
		var record struct {
			Event    string `json:"event"`
			Cap      int64  `json:"runtime_cap_bytes"`
			Refusals uint64 `json:"refusals"`
		}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		if record.Cap != disk.Cloud.PartBytes || i == 0 && record.Event != "foundry_rehearsal_storage" || i == 1 && (record.Event != "foundry_rehearsal_storage_refusal" || record.Refusals != 1) {
			t.Fatal("unexpected rehearsal record", record)
		}
	}
}
