package configuredprofile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/config/toml"
	"github.com/weiloon1234/Foundry-Go/logging"
)

func TestConfiguredLogRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.jsonl")
	schema, err := application.SettingsConfigSchema()
	if err != nil {
		t.Fatal(err)
	}
	document := fmt.Sprintf(`[log.channels.default.sink]
driver = 'file'
path = %q
[log.channels.default.sink.rotation]
max_bytes = 256
max_files = 2
max_age = '336h'
`, path)
	layer, err := toml.Decode(strings.NewReader(document), schema, toml.Options{Name: "logging"})
	if err != nil {
		t.Fatal(err)
	}
	settings, _, err := schema.Load(Defaults(), config.Inputs[application.Settings]{Files: []config.Values{layer}})
	if err != nil {
		t.Fatal(err)
	}
	policy := settings.Log.Channels["default"].Sink.Rotation
	if policy.MaxBytes != 256 || policy.MaxFiles != 2 || policy.MaxAge != 14*24*time.Hour {
		t.Fatal("typed rotation settings were not loaded")
	}
	settings.HTTP.Enabled = false
	app, err := application.New(settings).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(t, app) })
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("Build opened file")
	}
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	logger := app.Resources().Logger
	for i := 0; i < 20; i++ {
		logger.InfoContext(t.Context(), "consumer log entry", "index", i, "password", "never-write-this")
	}
	stop(t, app)
	archives, err := filepath.Glob(path + ".foundry-*.log")
	if err != nil || len(archives) != 2 {
		t.Fatal("configured retention not enforced", err)
	}
	for _, name := range append(archives, path) {
		data, err := os.ReadFile(name)
		if err != nil || len(data) > 256 || strings.Contains(string(data), "never-write-this") {
			t.Fatal("rotated output lost bounds or redaction", err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			if !json.Valid([]byte(line)) {
				t.Fatal("rotation split a structured record")
			}
		}
	}
}

func TestGeneratedLogRotationOverrides(t *testing.T) {
	schema, err := logging.ChannelSettingsConfigSchema()
	if err != nil {
		t.Fatal(err)
	}
	keys := logging.ChannelSettingsConfigKeys()
	settings, _, err := schema.Load(logging.ChannelSettings{Sink: logging.SinkConfig{Driver: logging.File, Path: filepath.Join(t.TempDir(), "app.log")}}, config.Inputs[logging.ChannelSettings]{
		Prefix: "LOG",
		Environment: func(name string) (string, bool) {
			if name == "LOG__SINK__ROTATION__MAX_FILES" {
				return "3", true
			}
			return "", false
		},
		Overrides: []config.Override[logging.ChannelSettings]{keys.Sink.Rotation.MaxBytes.Set(4096)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if settings.Sink.Rotation.MaxFiles != 3 || settings.Sink.Rotation.MaxBytes != 4096 {
		t.Fatal("environment or typed override ignored")
	}
}
