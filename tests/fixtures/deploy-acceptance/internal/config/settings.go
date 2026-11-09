package config

import (
	"fmt"
	"os"
	"time"

	"github.com/weiloon1234/Foundry-Go/application"
	foundryconfig "github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/config/toml"
)

// BuildRelease is set by the normal deployment builder.
var BuildRelease = "development"

//foundry:config
type Settings struct {
	App       application.Settings
	Project   ProjectSettings
	Fixture   FixtureSettings
	Transport TransportSettings
	Web       WebSettings
	Developer DeveloperSettings
}

type ProjectSettings struct{ Name, Env string }
type FixtureSettings struct{ Readiness string }
type TransportSettings struct{ UploadDirectory string }
type WebSettings struct{ PublicDir string }
type DeveloperSettings struct{ LogDirectory, SourceMapDirectory, Release string }

func Defaults() Settings {
	app := application.DefaultSettings()
	app.ShutdownTimeout = 35 * time.Second
	app.HTTP.Server.ShutdownTimeout = 20 * time.Second
	app.HTTP.Probes.Readiness = true
	app.Features.Health.Enabled = true
	app.Worker.Config.DrainTimeout = 20 * time.Second
	return Settings{App: app, Project: ProjectSettings{Name: "deployacceptance", Env: "testing"},
		Fixture: FixtureSettings{Readiness: "healthy"}, Developer: DeveloperSettings{Release: BuildRelease}}
}

func Load(path, envFile string) (Settings, foundryconfig.Report, error) {
	schema, err := SettingsConfigSchema()
	if err != nil {
		return Settings{}, foundryconfig.Report{}, err
	}
	inputs := foundryconfig.Inputs[Settings]{Environment: os.LookupEnv, EnvFile: foundryconfig.EnvFile{Path: envFile}}
	var settings Settings
	var report foundryconfig.Report
	if path == "" {
		settings, report, err = schema.Load(Defaults(), inputs)
	} else {
		settings, report, err = toml.LoadFile(path, schema, Defaults(), inputs, toml.Options{})
	}
	if err != nil {
		return Settings{}, foundryconfig.Report{}, err
	}
	switch settings.Fixture.Readiness {
	case "healthy", "fail", "hold":
	default:
		return Settings{}, foundryconfig.Report{}, fmt.Errorf("fixture readiness must be healthy, fail or hold")
	}
	settings.Developer.Release = BuildRelease
	return settings, report, nil
}
