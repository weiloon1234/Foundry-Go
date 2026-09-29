package application

import (
	"context"
	"errors"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/logging"
	"github.com/weiloon1234/Foundry-Go/schedule"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"log/slog"
	"slices"
	"sync"
)

// Routes constructs only domain handlers from typed resources; the framework
// owns router/server creation. It runs once at Build and must perform no I/O.
type Routes func(Services) ([]http.RouteRegistration, error)
type Builder struct{ state *builderState }
type builderState struct {
	mu         sync.Mutex
	settings   Settings
	options    options
	routes     []Routes
	middleware []http.Middleware
	observers  []http.RequestObserver
	providers  []foundation.Provider
	plugins    []foundation.Plugin
	jobs       []JobDeclaration
	events     []EventDeclaration
	schedules  []Schedules
	realtime   Realtime
	features   []Features
	err        error
	built      bool
}

// New snapshots configuration. Build returns any invalid settings/option error.
func New(settings Settings, opts ...Option) *Builder {
	b := &Builder{state: &builderState{options: options{clock: clock.System{}}}}
	schema, err := SettingsConfigSchema()
	if err == nil {
		b.state.settings, _, err = schema.Load(settings, config.Inputs[Settings]{})
	}
	b.state.err = err
	for _, option := range opts {
		if b.state.err != nil {
			break
		}
		if option == nil {
			b.state.err = fault.New(fault.Invalid, "nil application option")
			break
		}
		b.state.err = option(&b.state.options)
	}
	return b
}
func (b *Builder) mutate(fn func()) {
	if b == nil || b.state == nil {
		return
	}
	b.state.mu.Lock()
	defer b.state.mu.Unlock()
	if b.state.built {
		b.state.err = fault.New(fault.Closed, "application builder is already built")
		return
	}
	if b.state.err == nil {
		fn()
	}
}
func (b *Builder) HTTP(routes ...Routes) *Builder {
	b.mutate(func() {
		for _, r := range routes {
			if r == nil {
				b.state.err = fault.New(fault.Invalid, "nil application routes")
				return
			}
		}
		if len(routes) > 128-len(b.state.routes) {
			b.state.err = fault.New(fault.Invalid, "too many route constructors")
			return
		}
		b.state.routes = append(b.state.routes, routes...)
	})
	return b
}

// Use applies global middleware in declaration order, including router misses.
func (b *Builder) Use(middleware ...http.Middleware) *Builder {
	b.mutate(func() { b.state.middleware = append(b.state.middleware, middleware...) })
	return b
}
func (b *Builder) ObserveHTTP(observers ...http.RequestObserver) *Builder {
	b.mutate(func() { b.state.observers = append(b.state.observers, observers...) })
	return b
}
func (b *Builder) Register(providers ...foundation.Provider) *Builder {
	b.mutate(func() { b.state.providers = append(b.state.providers, providers...) })
	return b
}
func (b *Builder) RegisterPlugin(plugins ...foundation.Plugin) *Builder {
	b.mutate(func() { b.state.plugins = append(b.state.plugins, plugins...) })
	return b
}
func (b *Builder) Build(ctx context.Context) (*App, error) {
	if b == nil || b.state == nil || ctx == nil {
		return nil, fault.New(fault.Invalid, "application build needs a builder and context")
	}
	b.state.mu.Lock()
	if b.state.built {
		b.state.mu.Unlock()
		return nil, fault.New(fault.Closed, "application builder is already built")
	}
	b.state.built = true
	settings, configured, routes, middleware, observers, providers, plugins, buildErr := b.state.settings, b.state.options, slices.Clone(b.state.routes), slices.Clone(b.state.middleware), slices.Clone(b.state.observers), slices.Clone(b.state.providers), slices.Clone(b.state.plugins), b.state.err
	jobDeclarations, eventDeclarations, schedules, realtime, features := slices.Clone(b.state.jobs), slices.Clone(b.state.events), slices.Clone(b.state.schedules), b.state.realtime, slices.Clone(b.state.features)
	b.state.mu.Unlock()
	if buildErr != nil {
		return nil, buildErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s := settings
	if s.TimeZone == "" {
		s.TimeZone = temporal.UTC
	}
	dates, err := temporal.NewService(configured.clock, s.TimeZone)
	if err != nil {
		return nil, err
	}
	calendar, err := schedule.NewCalendar(s.TimeZone)
	if err != nil {
		return nil, err
	}
	if err := validateShutdownBudget(s); err != nil {
		return nil, err
	}
	if !s.HTTP.Enabled && (len(routes) > 0 || len(middleware) > 0 || len(observers) > 0) {
		return nil, fault.New(fault.Invalid, "HTTP declarations require HTTP to be enabled")
	}
	channels, err := logging.PrepareChannels(s.Log.Default, s.Log.inTimeZone(s.TimeZone), configured.logger, configured.logHandlers...)
	if err != nil {
		return nil, err
	}
	logger, err := channels.Channels().Default()
	if err != nil {
		return nil, err
	}
	s.Features, err = prepareFeatureSettings(s, configured.clock)
	if err != nil {
		return nil, err
	}
	if s.Worker.Archive, err = prepareJobArchive(s); err != nil {
		return nil, err
	}
	plan, err := infrastructure.Configure(s.Services, append(slices.Clone(configured.infrastructure), infrastructure.WithClock(configured.clock), infrastructure.WithLogger(logger))...)
	if err != nil {
		return nil, err
	}
	options := []foundation.Option{foundation.WithClock(configured.clock), foundation.WithLogger(logger), foundation.WithShutdownTimeout(s.ShutdownTimeout), foundation.WithStopDelay(s.StopDelay), foundation.WithStartupTimeout(s.StartupTimeout)}
	gate, err := prepareMaintenance(s, configured)
	if err != nil {
		return nil, err
	}
	options = append(options, foundation.WithMaintenance(gate))
	recorder, err := prepareObservation(s.Features.Observability, configured, logger, gate)
	if err != nil {
		return nil, err
	}
	if recorder != nil {
		options = append(options, foundation.WithObservability(recorder))
	}
	migrations, err := featureMigrations(plan, s.Features, s.Worker.Archive)
	if err != nil {
		return nil, err
	}
	builder := foundation.NewBuilder(options...)
	builder.Register(foundation.Module{Name: "foundry.application.logging", OnBoot: func(ctx context.Context, r *foundation.Runtime) error {
		if err := r.OnShutdown("channels", func(context.Context) error { return channels.Close() }); err != nil {
			return errors.Join(err, channels.Close())
		}
		return channels.Start(ctx)
	}})
	plan.Register(builder)
	registerResources(builder, s.Image, logger, channels.Channels(), s.Features, configured.clock, recorder, dates, calendar)
	builder.Register(providers...).RegisterPlugin(plugins...)
	if err := registerFeatures(ctx, builder, s, configured.clock, features); err != nil {
		return nil, err
	}
	if err := registerKernelDeclarations(builder, plan, s, configured.clock, jobDeclarations, eventDeclarations, schedules, realtime); err != nil {
		return nil, err
	}
	registerMaintenance(builder, s.Maintenance, gate, logger)
	if probes, err := registerProbes(builder, s); err != nil {
		return nil, err
	} else if probes != nil {
		routes = append(routes, probes)
	}
	if mount := realtimeRoutes(s); mount != nil {
		routes = append(routes, mount)
	}
	registerHTTP(builder, s.HTTP, s.Features.Locales.Enabled, stickyReadsConfigured(s.Services.Database), routes, middleware, observers)
	registerMetricsCollectors(builder, recorder, channels, s.Realtime.Enabled)
	app, err := builder.Build(ctx)
	if err != nil {
		return nil, err
	}
	resources, err := FromResolver(app.Services())
	if err != nil {
		return nil, err
	}
	result := &App{App: app, resources: resources, migrations: migrations}
	if s.HTTP.Enabled {
		result.server, err = foundation.Resolve(app.Services(), HTTPKey)
	}
	return result, err
}
func (Builder) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("application builder")) }
func (Builder) LogValue() slog.Value       { return slog.StringValue("application builder") }

// validateShutdownBudget keeps each enabled listener's drain inside the single
// application budget so cleanup always receives part of it.
func validateShutdownBudget(s Settings) error {
	if s.ShutdownTimeout <= 0 || s.StopDelay < 0 || s.StartupTimeout < 0 {
		return fault.New(fault.Invalid, "application shutdown/startup timeouts must be positive and the stop delay non-negative")
	}
	if s.HTTP.Enabled && s.StopDelay+s.HTTP.Server.ShutdownTimeout >= s.ShutdownTimeout {
		return fault.New(fault.Invalid, "HTTP shutdown grace plus stop delay must be shorter than the application shutdown timeout")
	}
	if s.Realtime.Enabled && !s.Realtime.Shared && s.StopDelay+s.Realtime.HTTP.ShutdownTimeout+s.Realtime.Config.DrainTimeout >= s.ShutdownTimeout {
		return fault.New(fault.Invalid, "realtime shutdown grace plus stop delay must be shorter than the application shutdown timeout")
	}
	if s.StopDelay >= s.ShutdownTimeout {
		return fault.New(fault.Invalid, "stop delay must be shorter than the application shutdown timeout")
	}
	return nil
}
