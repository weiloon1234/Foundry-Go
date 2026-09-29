package foundation

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/dependency"
	"github.com/weiloon1234/Foundry-Go/logging"
	"github.com/weiloon1234/Foundry-Go/maintenance"
	"github.com/weiloon1234/Foundry-Go/observability"
)

// DefaultShutdownTimeout is the whole shutdown budget: stop delay, kernel drain
// and every cleanup. It exceeds the default HTTP shutdown grace and fits a common
// 30-second orchestrator termination period. It bounds waits, not the lifetime
// of a goroutine ignoring cancellation; such work remains tracked until it exits.
const DefaultShutdownTimeout = 25 * time.Second

// Option configures an application builder.
type Option func(*settings) error
type settings struct {
	logger          *slog.Logger
	shutdownTimeout time.Duration
	stopDelay       time.Duration
	startupTimeout  time.Duration
	clock           clock.Clock
	observability   *observability.Recorder
	maintenance     *maintenance.Gate
}

// WithClock injects application time without changing lifecycle deadlines.
func WithClock(source clock.Clock) Option {
	return func(s *settings) error {
		if nilValue(source) {
			return fault.New(fault.Invalid, "clock cannot be nil")
		}
		s.clock = source
		return nil
	}
}

// WithLogger injects an application logger without changing slog.Default.
func WithLogger(logger *slog.Logger) Option {
	return func(s *settings) error {
		if logger == nil {
			return fault.New(fault.Invalid, "logger cannot be nil")
		}
		s.logger = logger
		return nil
	}
}

// WithShutdownTimeout sets the whole shutdown budget. It starts when shutdown
// begins and covers the stop delay, kernel drain and all cleanups, which share
// its remaining deadline. Run waits for this budget; manual Shutdown calls also
// respect the deadline on the caller's context. Configure kernel drain periods
// (for example the HTTP shutdown grace) below it.
func WithShutdownTimeout(timeout time.Duration) Option {
	return func(s *settings) error {
		if timeout <= 0 {
			return fault.New(fault.Invalid, "shutdown timeout must be positive")
		}
		s.shutdownTimeout = timeout
		return nil
	}
}

// WithStopDelay configures a lame-duck period for requested shutdown of a
// running application. State reports Stopping (so readiness fails) while
// kernels keep serving; afterwards admission closes and kernels drain. Kernel
// completion or failure stops immediately. The delay is part of the shutdown
// budget and must be shorter than it. Zero, the default, disables the delay.
func WithStopDelay(delay time.Duration) Option {
	return func(s *settings) error {
		if delay < 0 {
			return fault.New(fault.Invalid, "stop delay cannot be negative")
		}
		s.stopDelay = delay
		return nil
	}
}

// WithStartupTimeout bounds provider boot. Expiry cancels the application
// lifetime and Start reports fault.Timeout; boot callbacks remain owned until
// they return. Zero, the default, leaves startup bounded only by its context.
func WithStartupTimeout(timeout time.Duration) Option {
	return func(s *settings) error {
		if timeout < 0 {
			return fault.New(fault.Invalid, "startup timeout cannot be negative")
		}
		s.startupTimeout = timeout
		return nil
	}
}

// WithMaintenance supplies the application's admission gate, for example one
// shared with a fleet-wide maintenance store. Without it the application uses
// its recorder's gate, or a fresh gate when observability is not configured.
// A recorder configured with a different gate is rejected at Build.
func WithMaintenance(gate *maintenance.Gate) Option {
	return func(s *settings) error {
		if gate == nil {
			return fault.New(fault.Invalid, "maintenance gate cannot be nil")
		}
		s.maintenance = gate
		return nil
	}
}

// Builder is a single-use application composition. Build freezes registration;
// create a fresh builder through your bootstrap function for each application.
type Builder struct {
	mu        sync.Mutex
	settings  settings
	providers []providerEntry
	overrides []contributionOverride
	index     map[ProviderID]int
	err       error
	built     bool
}
type providerEntry struct {
	id           ProviderID
	dependencies []ProviderID
	provider     Provider
}

// NewBuilder constructs independent foundation state for explicit providers.
// Applications can use root foundry.New for this same direct assembly path, or
// application.New for configured service and HTTP assembly. Feature and testing
// packages use NewBuilder without depending on either application entry point.
func NewBuilder(options ...Option) *Builder {
	b := &Builder{index: make(map[ProviderID]int), settings: settings{
		logger: logging.JSON(os.Stderr, logging.Options{}), shutdownTimeout: DefaultShutdownTimeout, clock: clock.System{},
	}}
	for _, option := range options {
		if option == nil {
			b.err = fault.New(fault.Invalid, "nil builder option")
			break
		}
		if err := option(&b.settings); err != nil {
			b.err = err
			break
		}
	}
	return b
}

// Register adds providers in deterministic declaration order. Dependency order
// takes precedence. Duplicate IDs fail unless Replace is explicitly requested.
func (b *Builder) Register(providers ...Provider) *Builder {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, provider := range providers {
		b.add(provider, false)
	}
	return b
}

// Replace explicitly replaces an already declared provider, preserving its
// declaration position. Replacing a missing ID is an error, not an insertion.
func (b *Builder) Replace(provider Provider) *Builder {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.add(provider, true)
	return b
}

func (b *Builder) add(provider Provider, replace bool) {
	if b.err != nil {
		return
	}
	if b.built {
		b.err = fault.New(fault.Closed, "builder has already been built")
		return
	}
	if nilValue(provider) {
		b.err = fault.New(fault.Invalid, "nil provider")
		return
	}
	var entry providerEntry
	b.err = invoke("read provider declaration", func() error {
		entry = providerEntry{id: provider.ID(), provider: provider}
		if dependent, ok := provider.(Dependent); ok {
			entry.dependencies = append([]ProviderID(nil), dependent.Dependencies()...)
		}
		return nil
	})
	if b.err != nil {
		return
	}
	if !validName(string(entry.id)) {
		b.err = fault.New(fault.Invalid, "invalid provider ID")
		return
	}
	if strings.HasPrefix(string(entry.id), "plugin:") {
		if _, ok := provider.(pluginProvider); !ok {
			b.err = fault.New(fault.Invalid, "plugin provider IDs require RegisterPlugin")
			return
		}
	}
	position, exists := b.index[entry.id]
	if replace {
		if !exists {
			b.err = fault.New(fault.Missing, "cannot replace missing provider "+string(entry.id))
			return
		}
		if _, plugin := b.providers[position].provider.(pluginProvider); plugin {
			b.err = fault.New(fault.Invalid, "cannot replace a plugin as an application provider")
			return
		}
		b.providers[position] = entry
		return
	}
	if exists {
		b.err = fault.New(fault.Duplicate, "provider "+string(entry.id)+" is already registered")
		return
	}
	b.index[entry.id] = len(b.providers)
	b.providers = append(b.providers, entry)
}

// Build validates the provider graph, freezes registrations and resolves every
// service constructor before returning. It never boots a provider or kernel.
func (b *Builder) Build(ctx context.Context) (*App, error) {
	b.mu.Lock()
	if b.built {
		b.mu.Unlock()
		return nil, fault.New(fault.Closed, "builder has already been built")
	}
	b.built = true
	entries, overrides, settings, buildErr := append([]providerEntry(nil), b.providers...), append([]contributionOverride(nil), b.overrides...), b.settings, b.err
	b.mu.Unlock()
	if buildErr != nil {
		return nil, buildErr
	}
	if settings.stopDelay >= settings.shutdownTimeout {
		return nil, fault.New(fault.Invalid, "stop delay must be shorter than the shutdown timeout")
	}
	if recorder := settings.observability; recorder != nil {
		if settings.maintenance == nil {
			settings.maintenance = recorder.Gate()
		} else if settings.maintenance != recorder.Gate() {
			return nil, fault.New(fault.Invalid, "maintenance gate differs from the observation recorder's gate")
		}
	}
	if settings.maintenance == nil {
		settings.maintenance = &maintenance.Gate{}
	}
	ordered, registry, err := registerProviders(ctx, entries, overrides)
	if err != nil {
		return nil, err
	}
	services, err := registry.construct()
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	app := newApp(settings, ordered, services, registry.kernels)
	app.inspection = inspectRegistry(ordered, registry)
	return app, nil
}

func orderProviders(entries []providerEntry) ([]providerEntry, error) {
	byID := make(map[ProviderID]providerEntry, len(entries))
	roots := make([]ProviderID, 0, len(entries))
	for _, entry := range entries {
		for _, required := range entry.dependencies {
			if !validName(string(required)) {
				return nil, fault.New(fault.Invalid, "invalid dependency in provider "+string(entry.id))
			}
		}
		byID[entry.id] = entry
		roots = append(roots, entry.id)
	}
	order, failure := dependency.Order(roots, func(id ProviderID) ([]ProviderID, bool) { entry, ok := byID[id]; return entry.dependencies, ok })
	if failure != nil {
		if failure.Kind == dependency.Cycle {
			return nil, fault.New(fault.Cycle, "provider dependency cycle at "+string(failure.Key))
		}
		return nil, fault.New(fault.Missing, "provider dependency "+string(failure.Key)+" is not registered")
	}
	ordered := make([]providerEntry, 0, len(order))
	for _, id := range order {
		ordered = append(ordered, byID[id])
	}
	return ordered, nil
}
