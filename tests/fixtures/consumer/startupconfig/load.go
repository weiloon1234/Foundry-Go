package startupconfig

import (
	"os"
	"time"

	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/config/toml"
)

// doc-example:begin load-configuration
func Load(path string) (Settings, config.Report, error) {
	schema, err := SettingsConfigSchema()
	if err != nil {
		return Settings{}, config.Report{}, err
	}
	keys := SettingsConfigKeys()
	return toml.LoadFile(path, schema, Defaults(), config.Inputs[Settings]{
		Environment: os.LookupEnv, Environ: os.Environ, Prefix: "APP",
		Overrides: []config.Override[Settings]{keys.HTTP.Timeout.Set(5 * time.Second)},
		Validate:  Validate,
	}, toml.Options{})
}

// doc-example:end load-configuration

// doc-example:begin load-dotenv
func LoadEnvFile(envPath string, overrides ...config.Override[Settings]) (Settings, config.Report, error) {
	schema, err := SettingsConfigSchema()
	if err != nil {
		return Settings{}, config.Report{}, err
	}
	return schema.Load(Defaults(), config.Inputs[Settings]{
		EnvFile:     config.EnvFile{Path: envPath, Name: "local.env"},
		Environment: os.LookupEnv, Prefix: "APP", Overrides: overrides, Validate: Validate,
	})
}

// doc-example:end load-dotenv
