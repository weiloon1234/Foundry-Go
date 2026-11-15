package application

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/cloud/credentials"
	"github.com/weiloon1234/Foundry-Go/deployment"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/internal/deployrehearsal"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestRehearsalApplicationStartupNoticeAndRuntimeRefusal(t *testing.T) {
	t.Setenv("FOUNDRY_REHEARSAL_RUN_ID", strings.Repeat("a", 32))
	s, disk := rehearsalApplicationSettings()
	log, err := os.CreateTemp(t.TempDir(), "startup")
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	previous := os.Stderr
	os.Stderr = log
	defer func() { os.Stderr = previous }()
	builder := New(s, WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	// Inject the sealed grant at the assembly boundary; descriptor ownership and
	// matching are exercised through the deployment reader's separate regressions.
	builder.state.options.infrastructure = append(builder.state.options.infrastructure, infrastructure.WithRehearsal(deployrehearsal.New(map[storage.DiskID]int64{"default": disk.Cloud.PartBytes})))
	app, err := builder.Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer app.Shutdown(t.Context())
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	services, err := FromResolver(app.Services())
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

func rehearsalApplicationSettings() (Settings, infrastructure.DiskSettings) {
	s := DefaultSettings()
	s.HTTP.Enabled = false
	disk := infrastructure.DefaultDiskSettings()
	disk.Driver = infrastructure.CompatibleDisk
	disk.Cloud.Bucket, disk.Cloud.Region, disk.Cloud.Endpoint = "fixture-bucket", "us-east-1", "https://unused.example.test"
	disk.Cloud.Credentials = "rehearsal"
	disk.Cloud.Compatible.ConditionalCreate = true
	disk.Cloud.Compatible.SingleRequestConditions = true
	s.Services.Credentials = infrastructure.CredentialSources{"rehearsal": {Mode: credentials.Static, AccessKey: secret.New("private-access"), SecretKey: secret.New("private-secret")}}
	s.Services.Storage.Disks = infrastructure.Disks{"default": disk}
	return s, disk
}

func TestRehearsalIdentityWithoutDescriptorKeepsProductionCapabilities(t *testing.T) {
	t.Setenv(deployment.RehearsalRunEnvironment, strings.Repeat("a", 32))
	t.Setenv(deployment.BindingsEnvironment, "")
	if err := os.Unsetenv(deployment.BindingsEnvironment); err != nil {
		t.Fatal(err)
	}
	s, disk := rehearsalApplicationSettings()
	app, err := New(s, WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer app.Shutdown(t.Context())
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	services, err := FromResolver(app.Services())
	if err != nil {
		t.Fatal(err)
	}
	runtimeDisk, err := services.Storage.Disk("default")
	if err != nil {
		t.Fatal(err)
	}
	caps := runtimeDisk.Capabilities()
	if caps.ConditionalWriteMode != "" || caps.ConditionalWriteMaxBytes != disk.Cloud.PartBytes {
		t.Fatal("environment granted rehearsal")
	}
	if err := caps.ValidatePutPolicy(storage.PutOptions{Condition: storage.IfAbsent(), Size: value.Set[int64](500 << 20)}); !errors.Is(err, storage.Unsupported) {
		t.Fatal("production startup limit bypassed", err)
	}
}
