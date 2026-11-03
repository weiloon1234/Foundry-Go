package application_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/storage/local"
	"github.com/weiloon1234/Foundry-Go/storage/probe"
)

func TestProbeStorageLocalAndUnrelatedConnections(t *testing.T) {
	s := settings()
	s.HTTP.Enabled = false
	disk := infrastructure.DefaultDiskSettings()
	disk.Local.Root = t.TempDir()
	s.Services.Storage.Disks = infrastructure.Disks{"default": disk}
	database := infrastructure.DefaultConnectionSettings()
	database.Primary.Host, database.Primary.Port = "127.0.0.1", 1
	s.Services.Database.Connections = infrastructure.DatabaseConnections{"default": database}
	redis := infrastructure.DefaultRedisConnectionSettings()
	redis.Host, redis.Port = "127.0.0.1", 1
	s.Services.Redis.Connections = infrastructure.RedisConnections{"default": redis}
	builder := application.New(s, quiet())
	var output bytes.Buffer
	if err := application.ProbeStorage(t.Context(), builder, &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "{\"schema\":1,\"disks\":[{\"disk\":\"default\",\"outcome\":\"ok\"}]}\n" {
		t.Fatal(output.String())
	}
	backend, err := local.Open(t.Context(), local.DefaultConfig(disk.Local.Root))
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	page, err := backend.List(t.Context(), storage.ListOptions{Limit: 10})
	if err != nil || len(page.Objects) != 0 {
		t.Fatal("probe left an object", page, err)
	}
	// A second invocation proves the helper did not consume the builder.
	output.Reset()
	if err := application.ProbeStorage(t.Context(), builder, &output); err != nil {
		t.Fatal(err)
	}
}
func TestProbeStorageFailedDiskUsesFailureStatusAndContinues(t *testing.T) {
	s := settings()
	s.HTTP.Enabled = false
	small, good := infrastructure.DefaultDiskSettings(), infrastructure.DefaultDiskSettings()
	small.Local.Root, good.Local.Root = t.TempDir(), t.TempDir()
	small.Config.MaxObjectBytes = 16
	s.Services.Storage.Default = "a-small"
	s.Services.Storage.Disks = infrastructure.Disks{"a-small": small, "z-good": good}
	var output bytes.Buffer
	err := application.ProbeStorage(t.Context(), application.New(s, quiet()), &output)
	if err == nil || cli.Status(err) != cli.Failure {
		t.Fatal("disk failure must not be usage status", err)
	}
	var report struct {
		Schema int            `json:"schema"`
		Disks  []probe.Result `json:"disks"`
	}
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Schema != 1 || len(report.Disks) != 2 || report.Disks[0].Disk != "a-small" || report.Disks[0].Outcome != "failed" || report.Disks[0].Stage != "write" || report.Disks[1] != (probe.Result{Disk: "z-good", Outcome: "ok"}) {
		t.Fatal(output.String())
	}
}
func TestProbeStorageEmptyAndNilBuilder(t *testing.T) {
	var output bytes.Buffer
	if err := application.ProbeStorage(t.Context(), nil, &output); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	s := settings()
	s.HTTP.Enabled = false
	builder := application.New(s, quiet())
	if err := application.ProbeStorage(t.Context(), builder, &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "{\"schema\":1,\"disks\":[]}\n" {
		t.Fatal(output.String())
	}
	app, err := builder.Build(t.Context())
	if err != nil {
		t.Fatal("probe consumed builder", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
}
