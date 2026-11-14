package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	fixtureconfig "foundry.test/deployacceptance/internal/config"
	"foundry.test/deployacceptance/internal/work"
	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
)

func TestRedisKeysDefaultsToDeploymentJSON(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "config", "deploy.toml"))
	if err != nil {
		t.Fatal(err)
	}
	// Bootstrap supplies storage credentials on the host; this command needs no services.
	path := filepath.Join(t.TempDir(), "deploy.toml")
	if err := os.WriteFile(path, []byte(strings.Split(string(data), "[app.services.storage]")[0]), 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if err := Run(t.Context(), []string{"redis:keys", "--config", path}, cli.Streams{Out: &out, Err: &stderr}); err != nil {
		t.Fatal(err)
	}
	var actual []infrastructure.RedisConnectionInventory
	if err := json.Unmarshal(out.Bytes(), &actual); err != nil {
		t.Fatalf("deployment expects JSON without flags: %v", err)
	}
	settings, _, err := fixtureconfig.Load(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if settings.App.Maintenance.Store != "maintenance" {
		t.Fatal("deployment must select the shared maintenance store")
	}
	store := settings.App.Services.Cache.Stores[settings.App.Maintenance.Store]
	if store.Driver != infrastructure.RedisCache || store.Redis != "main" {
		t.Fatal("maintenance must use the main Redis connection")
	}
	expected, err := Builder(settings, work.NewReceipts(t.TempDir())).RedisKeys()
	if err != nil {
		t.Fatal(err)
	}
	if len(actual) != 2 || !reflect.DeepEqual(actual, expected) || stderr.Len() != 0 {
		t.Fatal("deployment inventory differs from its public owner")
	}
	out.Reset()
	if err := Run(t.Context(), []string{"config:check", "--config", path}, cli.Streams{Out: &out, Err: &stderr}); err != nil {
		t.Fatal(err)
	}
	if out.String() != "{\"valid\":true}\n" {
		t.Fatalf("unexpected config check response: %s", out.String())
	}
	for _, args := range [][]string{{"queue:work", "--config", path}, {"queue:work", "--config", path, "--connection", "wrong"}} {
		if err := Run(t.Context(), args, cli.Streams{Out: &out, Err: &stderr}); err == nil {
			t.Fatal("worker accepted a missing or mismatched connection")
		}
	}
}

func TestGlobalFlagsAfterSubcommand(t *testing.T) {
	args, path, env, err := globals([]string{"fixture:enqueue", "--queue", "first", "--config", "deploy.toml", "--id", "id", "--env-file=app.env", "--barrier"})
	if err != nil || path != "deploy.toml" || env != "app.env" || !reflect.DeepEqual(args, []string{"fixture:enqueue", "--queue", "first", "--id", "id", "--barrier"}) {
		t.Fatalf("unexpected parse: %v %q %q %v", args, path, env, err)
	}
	for _, args := range [][]string{{"serve", "--config"}, {"serve", "--env-file="}, {"serve", "--config", "one", "--config", "two"}} {
		if _, _, _, err := globals(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}

func TestConfigCheckWithRehearsalSchedulerOverride(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "config", "deploy.toml"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	path, env := filepath.Join(root, "deploy.toml"), filepath.Join(root, "app.env")
	// Keep database/Redis declarations pointing at unavailable local services:
	// graph validation must not start them. Storage credentials are host inputs.
	if err := os.WriteFile(path, []byte(strings.Split(string(data), "[app.services.storage]")[0]), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(env, []byte("APP__SCHEDULER__ENABLED=false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if err := Run(t.Context(), []string{"config:check", "--config", path, "--env-file", env}, cli.Streams{Out: &out, Err: &stderr}); err != nil {
		t.Fatal(err)
	}
	if out.String() != "{\"valid\":true}\n" || stderr.Len() != 0 {
		t.Fatal("unexpected rehearsal config check response")
	}
}

func TestReadinessHoldIsReadOnlyAndExplicitlyReleased(t *testing.T) {
	root := t.TempDir()
	receipts := work.NewReceipts(root)
	if err := readiness(t.Context(), "hold", receipts); err == nil {
		t.Fatal("held readiness succeeded")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatal("readiness wrote marker")
	}
	if err := receipts.Mark("readiness.release"); err != nil {
		t.Fatal(err)
	}
	if err := readiness(t.Context(), "hold", receipts); err != nil {
		t.Fatal(err)
	}
	if err := readiness(t.Context(), "fail", receipts); err == nil {
		t.Fatal("failed readiness succeeded")
	}
	if err := readiness(t.Context(), "healthy", receipts); err != nil {
		t.Fatal(err)
	}
}

func TestArchiveConfigurationAndRealDeclarations(t *testing.T) {
	settings, _, err := fixtureconfig.Load(filepath.Join("..", "..", "config", "deploy.toml"), "")
	if err != nil {
		t.Fatal(err)
	}
	if !settings.App.Worker.Enabled || !settings.App.Scheduler.Enabled || !settings.App.HTTP.Probes.Readiness || settings.App.HTTP.Probes.ReadinessPath == "/health/ready" {
		t.Fatal("missing runtime roles or readiness")
	}
	if settings.App.Services.Storage.Disks["attachments"].Driver != "r2" {
		t.Fatal("missing private object storage declaration")
	}
	// Bootstrap supplies cloud credentials and disks. Inventory without storage
	// exercises every Redis namespace without needing runtime secrets or services.
	settings.App.Services.Storage.Disks = nil
	builder := Builder(settings, work.NewReceipts(t.TempDir()))
	inventory, err := builder.RedisKeys()
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory) != 2 {
		t.Fatalf("got %d Redis connections", len(inventory))
	}
	built, err := builder.Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := shutdown(built); err != nil {
		t.Fatal(err)
	}
}

func TestMaintenanceCommandsShareStateAcrossInvocations(t *testing.T) {
	for _, schedulerEnabled := range []bool{false, true} {
		t.Run(fmt.Sprint(schedulerEnabled), func(t *testing.T) {
			testMaintenanceCommandsShareState(t, schedulerEnabled)
		})
	}
}

func testMaintenanceCommandsShareState(t *testing.T, schedulerEnabled bool) {
	path := filepath.Join(t.TempDir(), "maintenance.toml")
	configuration := fmt.Sprintf(`[app.scheduler]
enabled = %t
[app.services.coordination]
enabled = true
driver = "memory"
[app.services.cache]
default = "maintenance"
[app.maintenance]
store = "maintenance"
[app.services.cache.stores.maintenance]
driver = "file"
[app.services.cache.stores.maintenance.file]
root = %q
`, schedulerEnabled, t.TempDir())
	if err := os.WriteFile(path, []byte(configuration), 0600); err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		command string
		down    bool
	}{{"maintenance:status", false}, {"down", true}, {"maintenance:status", true}, {"up", false}, {"maintenance:status", false}} {
		var out, stderr bytes.Buffer
		if err := Run(t.Context(), []string{step.command, "--format", "json", "--config", path}, cli.Streams{In: strings.NewReader(""), Out: &out, Err: &stderr}); err != nil {
			t.Fatalf("%s: %v", step.command, err)
		}
		var state struct {
			Down bool `json:"down"`
		}
		if err := json.Unmarshal(out.Bytes(), &state); err != nil {
			t.Fatalf("%s: %v", step.command, err)
		}
		if state.Down != step.down {
			t.Fatalf("%s: down=%v, want %v", step.command, state.Down, step.down)
		}
	}
}
