// Package configuredprofile is compact executable acceptance, not a starter app.
package configuredprofile

import (
	"context"
	"foundry.test/consumer/productionprofile"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/config/toml"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"os"
)

const Primary cache.StoreName = "primary"
const Reports cache.StoreName = "reports"

// Defaults uses the existing generated application schema, without another schema.
func Defaults() application.Settings {
	s := application.DefaultSettings()
	s.HTTP.Server.Address = "127.0.0.1:0"
	s.Services.Cache.Default = Primary
	s.Services.Cache.Stores = infrastructure.CacheStores{Primary: infrastructure.DefaultCacheSettings(), Reports: infrastructure.DefaultCacheSettings()}
	return s
}
func Load(path string) (application.Settings, error) {
	schema, err := application.SettingsConfigSchema()
	if err != nil {
		return application.Settings{}, err
	}
	inputs := config.Inputs[application.Settings]{Environment: os.LookupEnv, Prefix: "PROFILE"}
	if path != "" {
		value, _, err := toml.LoadFile(path, schema, Defaults(), inputs, toml.Options{})
		return value, err
	}
	value, _, err := schema.Load(Defaults(), inputs)
	return value, err
}
func Build(ctx context.Context, settings application.Settings, options ...application.Option) (*application.App, error) {
	if err := productionprofile.Check(); err != nil {
		return nil, err
	}
	return application.New(settings, options...).HTTP(Routes).Build(ctx)
}
