package configuredprofile

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/config/toml"
	"github.com/weiloon1234/Foundry-Go/schedule"
)

// Deployment files enable the housekeeping schedule and tune one task; the
// application only declares its own store's bounded prune operation.
func TestConfiguredMaintenanceScheduleRunsDeclaredPruning(t *testing.T) {
	schema, err := application.SettingsConfigSchema()
	if err != nil {
		t.Fatal(err)
	}
	layer, err := toml.Decode(strings.NewReader(`[scheduler]
enabled = true
[services.coordination]
enabled = true
[features.maintenance]
enabled = true
[features.maintenance.custom]
batch = 25
interval = '15m'
`), schema, toml.Options{Name: "maintenance"})
	if err != nil {
		t.Fatal(err)
	}
	keys := application.SettingsConfigKeys()
	settings, _, err := schema.Load(Defaults(), config.Inputs[application.Settings]{Files: []config.Values{layer}, Overrides: []config.Override[application.Settings]{keys.Features.Maintenance.Custom.MaxBatches.Set(2)}})
	if err != nil {
		t.Fatal(err)
	}
	settings.HTTP.Enabled = false
	var mu sync.Mutex
	var limits []int
	exports := application.PruneWith("profile.exports", 100, func(_ context.Context, limit int) (int64, error) {
		mu.Lock()
		defer mu.Unlock()
		limits = append(limits, limit)
		return int64(limit), nil
	})
	app, err := application.New(settings).Features(func(application.Services) (application.FeatureDeclarations, error) {
		return application.FeatureDeclarations{Pruning: []application.Pruning{exports}}, nil
	}).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(t, app) })
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	scheduler, err := app.Resources().Scheduler()
	if err != nil {
		t.Fatal(err)
	}
	described := scheduler.Describe()
	if len(described) != 1 || described[0].ID != "foundry.maintenance.profile.exports" || described[0].Interval != 15*time.Minute {
		t.Fatal("configured maintenance schedule was not registered", described)
	}
	if record, err := scheduler.RunNow(t.Context(), described[0].ID); err != nil || record.State != schedule.Succeeded {
		t.Fatal("declared pruning failed", record, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(limits) != 2 || limits[0] != 25 || limits[1] != 25 {
		t.Fatal("deployment batch settings were not applied", limits)
	}
}
