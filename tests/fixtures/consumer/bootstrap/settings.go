// Package bootstrap is executable framework acceptance, not a starter product.
package bootstrap

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/config/toml"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"os"
)

//foundry:config
type Settings struct {
	App           application.Settings
	Schema        string
	Greeting      string
	MemberID      model.ID[Member]
	OperatorID    model.ID[Operator]
	MemberToken   secret.String
	OperatorToken secret.String
}

func Defaults() Settings {
	s := application.DefaultSettings()
	s.Services.Cache.Stores = infrastructure.CacheStores{"default": infrastructure.DefaultCacheSettings()}
	s.Image.Enabled = true
	return Settings{App: s, Schema: "public", Greeting: "Welcome"}
}
func Load(path string, overrides ...config.Override[Settings]) (Settings, error) {
	schema, err := SettingsConfigSchema()
	if err != nil {
		return Settings{}, err
	}
	inputs := config.Inputs[Settings]{Environment: os.LookupEnv, Prefix: "BOOTSTRAP", Overrides: overrides}
	if path == "" {
		settings, _, err := schema.Load(Defaults(), inputs)
		return settings, err
	}
	settings, _, err := toml.LoadFile(path, schema, Defaults(), inputs, toml.Options{})
	return settings, err
}
func Build(ctx context.Context, settings Settings, options ...application.Option) (*application.App, error) {
	if settings.Schema == "" || len(settings.Schema) > 63 || settings.MemberID.IsZero() || settings.OperatorID.IsZero() || settings.MemberToken.IsZero() || settings.OperatorToken.IsZero() {
		return nil, fault.New(fault.Invalid, "bootstrap acceptance needs explicit actors and schema")
	}
	// Schema is a SQL parameter below. It is never interpolated into domain SQL.
	return application.New(settings.App, options...).HTTP(func(services application.Services) ([]http.RouteRegistration, error) {
		return Routes(settings, services)
	}).Build(ctx)
}
