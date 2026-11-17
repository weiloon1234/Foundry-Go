package profiles_test

import (
	"os"
	"path/filepath"
	"testing"

	"foundry.test/consumer/bootstrap"
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/config/toml"
	"github.com/weiloon1234/Foundry-Go/imaging"
)

func TestImageBackendConfiguration(t *testing.T) {
	schema, err := bootstrap.SettingsConfigSchema()
	if err != nil {
		t.Fatal(err)
	}
	for _, backend := range imaging.BackendValues() {
		t.Run(string(backend), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "app.toml")
			body := "[app.image.config]\nbackend = \"" + string(backend) + "\"\n"
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			got, _, err := toml.LoadFile(path, schema, bootstrap.Defaults(), config.Inputs[bootstrap.Settings]{}, toml.Options{})
			if err != nil || got.App.Image.Config.Backend != backend {
				t.Fatal("TOML backend did not load", err)
			}
			got, _, err = schema.Load(bootstrap.Defaults(), config.Inputs[bootstrap.Settings]{
				Prefix: "IMAGE_TEST",
				Environment: func(key string) (string, bool) {
					return string(backend), key == "IMAGE_TEST__APP__IMAGE__CONFIG__BACKEND"
				},
			})
			if err != nil || got.App.Image.Config.Backend != backend {
				t.Fatal("environment backend did not load", err)
			}
		})
	}
	for _, raw := range []string{"0", "1", "2", `"0"`, `"1"`, `"2"`, `""`, `"unknown"`} {
		t.Run("reject_"+raw, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "app.toml")
			if err := os.WriteFile(path, []byte("[app.image.config]\nbackend = "+raw+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := toml.LoadFile(path, schema, bootstrap.Defaults(), config.Inputs[bootstrap.Settings]{}, toml.Options{}); err == nil {
				t.Fatal("invalid TOML backend accepted")
			}
		})
	}
	for _, raw := range []string{"", "0", "1", "2", "unknown"} {
		if _, _, err := schema.Load(bootstrap.Defaults(), config.Inputs[bootstrap.Settings]{
			Prefix: "IMAGE_TEST",
			Environment: func(key string) (string, bool) {
				return raw, key == "IMAGE_TEST__APP__IMAGE__CONFIG__BACKEND"
			},
		}); err == nil {
			t.Fatalf("invalid environment backend %q accepted", raw)
		}
	}
}
