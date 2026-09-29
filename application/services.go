package application

import (
	"context"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/imaging"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/logging"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/schedule"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"log/slog"
)

const Provider foundation.ProviderID = "foundry.application"
const ImageProvider foundation.ProviderID = "foundry.application.image"
const RouterProvider foundation.ProviderID = "foundry.application.router"
const HTTPProvider foundation.ProviderID = "foundry.application.http"

var servicesKey = foundation.NewKey[Services]("foundry.application.services")
var ImageKey = foundation.NewKey[*imaging.Engine]("foundry.application.image")
var RouterKey = foundation.NewKey[*http.Router]("foundry.application.router")
var HTTPKey = foundation.NewKey[*http.Server]("foundry.application.http")

// Services is a constructor input, never a request context value. Ordinary
// constructors receive concrete default/named handles from its typed accessors.
// Resolve is the explicit extension boundary for application-specific services.
type Services struct {
	*infrastructure.Services
	Logger   *slog.Logger
	Logs     *logging.Channels
	image    *imaging.Engine
	resolver foundation.Resolver
	features FeatureSettings
	clock    clock.Clock
	dates    temporal.Service
	calendar schedule.Calendar
	recorder *observability.Recorder
}

// Time provides the application's clock and timezone-bound date helpers.
func (s Services) Time() temporal.Service { return s.dates }

// Calendar constructs schedule declarations inheriting the application timezone.
func (s Services) Calendar() schedule.Calendar { return s.calendar }

func (s Services) Image() (*imaging.Engine, error) {
	if s.image == nil {
		return nil, fault.New(fault.Missing, "image processing is not configured")
	}
	return s.image, nil
}

// FromResolver provides constructor services bound to this active resolver.
// Custom provider factories use it while constructing concrete domain handles.
// Retaining constructor services does not extend the resolver's lifetime;
// App.Resources uses the application's frozen runtime resolver instead.
func FromResolver(r foundation.Resolver) (Services, error) {
	services, err := foundation.Resolve(r, servicesKey)
	if err != nil {
		return Services{}, err
	}
	services.resolver = r
	return services, nil
}

func Resolve[T any](s Services, key foundation.Key[T]) (T, error) {
	return foundation.Resolve(s.resolver, key)
}

// App retains the existing lifecycle and kernel contract. Each Build constructs
// independent resources. HTTPReady reports the actual bound listener address.
type App struct {
	*foundation.App
	resources  Services
	server     *http.Server
	migrations []infrastructure.MigrationTarget
}

func (a *App) Resources() Services { return a.resources }
func (a *App) HTTPReady(ctx context.Context) (string, error) {
	if a == nil || a.server == nil {
		return "", fault.New(fault.Missing, "HTTP is not configured")
	}
	return a.server.Ready(ctx)
}
func (a *App) Migrations() []infrastructure.MigrationTarget {
	if a == nil {
		return nil
	}
	return cloneMigrations(a.migrations)
}

func (Services) Format(s fmt.State, _ rune)               { _, _ = s.Write([]byte("application services")) }
func (Services) LogValue() slog.Value                     { return slog.StringValue("application services") }
func (s Services) Observability() *observability.Recorder { return s.recorder }
