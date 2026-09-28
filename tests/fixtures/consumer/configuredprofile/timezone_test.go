package configuredprofile

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/config/toml"
	"github.com/weiloon1234/Foundry-Go/logging"
	"github.com/weiloon1234/Foundry-Go/schedule"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

func TestConfiguredApplicationTimeZones(t *testing.T) {
	schema, err := application.SettingsConfigSchema()
	if err != nil {
		t.Fatal(err)
	}
	layer, err := toml.Decode(strings.NewReader("time_zone = 'Asia/Kuala_Lumpur'"), schema, toml.Options{Name: "application"})
	if err != nil {
		t.Fatal(err)
	}
	settings, _, err := schema.Load(Defaults(), config.Inputs[application.Settings]{Files: []config.Values{layer}})
	if err != nil || settings.TimeZone != "Asia/Kuala_Lumpur" {
		t.Fatal(settings.TimeZone, err)
	}
	keys := application.SettingsConfigKeys()
	overrides := []config.Override[application.Settings]{keys.TimeZone.Set(temporal.UTC)}
	other, _, err := schema.Load(settings, config.Inputs[application.Settings]{Overrides: overrides})
	if err != nil || other.TimeZone != temporal.UTC {
		t.Fatal(other.TimeZone, err)
	}
	env, _, err := schema.Load(settings, config.Inputs[application.Settings]{Prefix: "APP", Environment: func(name string) (string, bool) {
		if name == "APP__TIME_ZONE" {
			return "America/New_York", true
		}
		return "", false
	}})
	if err != nil || env.TimeZone != "America/New_York" {
		t.Fatal(env.TimeZone, err)
	}
	source := testkit.NewClock(time.Date(2026, 9, 25, 17, 30, 0, 0, time.UTC))
	hostZone := time.Local
	for _, tc := range []struct {
		name         temporal.ZoneName
		today, stamp string
	}{
		{settings.TimeZone, "2026-09-26", "2026-09-26T01:30:00+08:00"},
		{other.TimeZone, "2026-09-25", "2026-09-25T17:30:00Z"},
		{"", "2026-09-25", "2026-09-25T17:30:00Z"},
	} {
		t.Run(string(tc.name), func(t *testing.T) {
			t.Parallel()
			settings := Defaults()
			settings.HTTP.Enabled = false
			settings.TimeZone = tc.name
			settings.Scheduler.Enabled = true
			settings.Services.Coordination.Enabled = true
			path := filepath.Join(t.TempDir(), "app.jsonl")
			explicitPath := filepath.Join(t.TempDir(), "utc.jsonl")
			settings.Log.Channels = application.LogChannels{
				"default": {Sink: logging.SinkConfig{Driver: logging.File, Path: path}},
				"utc":     {Sink: logging.SinkConfig{Driver: logging.File, Path: explicitPath, TimeZone: temporal.UTC}},
			}
			var retained temporal.Service
			builder := application.New(settings, application.WithClock(source)).Schedules(func(s application.Services) ([]schedule.Declaration, error) {
				retained = s.Time()
				calendar := s.Calendar()
				declaration, err := calendar.DailyAt("report.daily", "00:00", func(context.Context, schedule.Invocation) error { return nil })
				if err != nil {
					return nil, err
				}
				if declaration.Spec().TimeZone() != string(retained.TimeZone()) {
					t.Fatal("calendar did not inherit app zone")
				}
				return []schedule.Declaration{declaration}, nil
			})
			settings.TimeZone = "Invalid/AfterSnapshot"
			app, err := builder.Build(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { stop(t, app) })
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("Build opened log file")
			}
			if err := app.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			dates := app.Resources().Time()
			today, err := dates.Today()
			if err != nil || today.String() != tc.today || dates.TimeZone() != retained.TimeZone() {
				t.Fatal(today, err)
			}
			record := slog.NewRecord(source.Now(), slog.LevelInfo, "timezone probe", 0)
			if err := app.Resources().Logger.Handler().Handle(t.Context(), record); err != nil {
				t.Fatal(err)
			}
			logger, err := app.Resources().Logs.Channel("utc")
			if err != nil {
				t.Fatal(err)
			}
			if err := logger.Handler().Handle(t.Context(), record); err != nil {
				t.Fatal(err)
			}
			stop(t, app)
			for name, stamp := range map[string]string{path: tc.stamp, explicitPath: "2026-09-25T17:30:00Z"} {
				data, err := os.ReadFile(name)
				if err != nil || !strings.Contains(string(data), `"time":"`+stamp+`"`) {
					t.Fatal("log timezone missing", err)
				}
			}
			if time.Local != hostZone {
				t.Fatal("global timezone mutated")
			}
		})
	}
}

func TestInvalidApplicationTimeZoneFailsBeforeResourceAcquisition(t *testing.T) {
	for _, name := range []temporal.ZoneName{"Local", "Invalid/Zone", "+24:00"} {
		settings := Defaults()
		settings.TimeZone = name
		path := filepath.Join(t.TempDir(), "never-created.log")
		settings.Log.Channels = application.LogChannels{"default": {Sink: logging.SinkConfig{Driver: logging.File, Path: path}}}
		if app, err := application.New(settings).Build(t.Context()); err == nil || app != nil {
			t.Fatal("invalid timezone built application")
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("invalid timezone acquired resources")
		}
	}
}
