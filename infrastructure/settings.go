// Package infrastructure assembles configured, named services over Foundry's
// existing providers. Configuration and graph validation are pure; resources are
// acquired only when the containing application boots.
package infrastructure

import (
	"context"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/logging"
	"log/slog"
	"maps"
	"os"
	"slices"

	cachefile "github.com/weiloon1234/Foundry-Go/cache/file"
	cachepg "github.com/weiloon1234/Foundry-Go/cache/postgres"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/cloud/credentials"
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
	"github.com/weiloon1234/Foundry-Go/internal/namedservice"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/storage/local"
)

// Settings enables each family by supplying a nonempty collection. Its Default
// names an existing entry; omitted backend references select that default.
//
//foundry:config
type Settings struct {
	Namespace    keyspace.Namespace
	Database     DatabaseSettings
	Redis        RedisSettings
	Storage      StorageSettings
	Cache        CachesSettings
	Mail         MailSettings
	Jobs         JobsSettings
	HTTPClients  HTTPClientsSettings
	PubSub       PubSubSettings
	Realtime     RealtimeSettings
	Coordination CoordinationSettings
	Credentials  CredentialSources `config:",secret"`
}

func DefaultSettings() Settings {
	return Settings{Namespace: keyspace.Namespace{Application: "app", Environment: "development"}, Database: DefaultDatabaseSettings(), Redis: DefaultRedisSettings(), Storage: DefaultStorageSettings(), Cache: DefaultCachesSettings(), Mail: DefaultMailSettings(), Jobs: DefaultJobsSettings(), HTTPClients: DefaultHTTPClientsSettings(), PubSub: DefaultPubSubSettings(), Realtime: DefaultRealtimeSettings(), Coordination: DefaultCoordinationSettings()}
}

type options struct {
	clock       clock.Clock
	logger      *slog.Logger
	mailDrivers map[MailDriver]email.Driver
	credentials map[credentials.Name]credentials.Provider
}
type Option func(*options) error

func WithClock(source clock.Clock) Option {
	return func(o *options) error {
		if credential.IsNil(source) {
			return fault.New(fault.Invalid, "infrastructure clock is nil")
		}
		o.clock = source
		return nil
	}
}

// WithCredentials installs an explicitly borrowed rotating provider. Its caller
// owns lifecycle and keeps it alive until the whole application has drained.
func WithCredentials(name credentials.Name, provider credentials.Provider) Option {
	return func(o *options) error {
		if err := name.Validate(); err != nil {
			return err
		}
		if credential.IsNil(provider) {
			return fault.New(fault.Invalid, "cloud credential provider is nil")
		}
		if _, ok := o.credentials[name]; ok {
			return fault.New(fault.Duplicate, "cloud credential provider is duplicated")
		}
		o.credentials[name] = provider
		return nil
	}
}

type Plan struct {
	settings  Settings
	options   options
	providers []foundation.Provider
}

// Configure freezes a settings snapshot and constructs provider definitions. It
// does not open files, retrieve credentials, connect pools or execute migrations.
func Configure(settings Settings, optionsList ...Option) (*Plan, error) {
	o := options{clock: clock.System{}, logger: logging.JSON(os.Stderr, logging.Options{}), mailDrivers: make(map[MailDriver]email.Driver), credentials: make(map[credentials.Name]credentials.Provider)}
	for _, option := range optionsList {
		if option == nil {
			return nil, fault.New(fault.Invalid, "nil infrastructure option")
		}
		if err := option(&o); err != nil {
			return nil, err
		}
	}
	schema, err := SettingsConfigSchema()
	if err != nil {
		return nil, err
	}
	owned, _, err := schema.Load(settings, config.Inputs[Settings]{})
	if err != nil {
		return nil, err
	}
	p := &Plan{settings: owned, options: o}
	if err = p.validate(); err != nil {
		return nil, err
	}
	p.assemble()
	return p, nil
}

// Providers returns a fresh declaration slice. Every registration constructs
// independent services, so the same plan may be used by independent applications.
func (p *Plan) Providers() []foundation.Provider {
	if p == nil {
		return nil
	}
	return slices.Clone(p.providers)
}
func (p *Plan) Register(builder *foundation.Builder) *foundation.Builder {
	return builder.Register(p.Providers()...)
}
func keys[K ~string, V any](values map[K]V) []K { return slices.Sorted(maps.Keys(values)) }
func selection[N namedservice.Name, V any](field config.Key[Settings, N], selected N, values map[N]V) error {
	if len(values) > namedservice.MaxEntries {
		return fault.New(fault.Invalid, "too many named services for "+field.Name())
	}
	if len(values) == 0 {
		return nil
	}
	if err := selected.Validate(); err != nil {
		return fault.Wrap(fault.Invalid, "invalid default service selection for "+field.Name(), err)
	}
	if _, ok := values[selected]; !ok {
		return fault.New(fault.Missing, "default service for "+field.Name()+" is not configured")
	}
	for _, name := range keys(values) {
		if err := name.Validate(); err != nil {
			return fault.Wrap(fault.Invalid, "invalid named service for "+field.Name(), err)
		}
	}
	return nil
}
func (p *Plan) validate() error {
	s := &p.settings
	if err := s.Namespace.Validate(); err != nil {
		return err
	}
	fields := SettingsConfigKeys()
	for _, err := range []error{
		selection(fields.Database.Default, s.Database.Default, s.Database.Connections),
		selection(fields.Redis.Default, s.Redis.Default, s.Redis.Connections),
		selection(fields.Storage.Default, s.Storage.Default, s.Storage.Disks),
		selection(fields.Cache.Default, s.Cache.Default, s.Cache.Stores),
	} {
		if err != nil {
			return err
		}
	}
	remaining := s.Database.MaxConnections
	if len(s.Database.Connections) > 0 && (remaining <= 0 || remaining > 65536) {
		return fault.New(fault.Invalid, "invalid total database connection budget")
	}
	for _, name := range keys(s.Database.Connections) {
		c := s.Database.Connections[name]
		if err := c.Validate(); err != nil {
			return err
		}
		if c.MaxConnections > remaining {
			return fault.New(fault.Invalid, "database connections exceed total budget")
		}
		remaining -= c.MaxConnections
	}
	remaining = s.Redis.MaxConnections
	if len(s.Redis.Connections) > 0 && (remaining <= 0 || remaining > 65536) {
		return fault.New(fault.Invalid, "invalid total Redis connection budget")
	}
	for _, name := range keys(s.Redis.Connections) {
		c := s.Redis.Connections[name]
		if err := c.Validate(); err != nil {
			return err
		}
		if c.MaxConnections > remaining || c.MaxSubscriptions > remaining-c.MaxConnections {
			return fault.New(fault.Invalid, "Redis connections exceed total budget")
		}
		remaining -= c.MaxConnections + c.MaxSubscriptions
	}
	if len(s.Credentials)+len(p.options.credentials) > namedservice.MaxEntries {
		return fault.New(fault.Invalid, "too many credential sources")
	}
	for _, name := range keys(s.Credentials) {
		if err := name.Validate(); err != nil {
			return err
		}
		if err := s.Credentials[name].Validate(); err != nil {
			return err
		}
		if _, ok := p.options.credentials[name]; ok {
			return fault.New(fault.Duplicate, "cloud credential source is duplicated")
		}
	}
	for _, name := range keys(s.Storage.Disks) {
		disk := s.Storage.Disks[name]
		if err := disk.Config.Validate(); err != nil {
			return err
		}
		switch disk.Driver {
		case LocalDisk:
			c := local.DefaultConfig(disk.Local.Root)
			c.Clock = p.options.clock
			c.MaxObjectBytes = disk.Config.MaxObjectBytes
			c.MaxScan = disk.Local.MaxScan
			c.Sync = disk.Local.Sync
			if err := c.Validate(); err != nil {
				return err
			}
		case S3Disk, R2Disk, CompatibleDisk:
			var provider credentials.Provider
			if disk.Cloud.Credentials != "" {
				provider = p.options.credentials[disk.Cloud.Credentials]
				if provider == nil {
					c, ok := s.Credentials[disk.Cloud.Credentials]
					if !ok {
						return fault.New(fault.Missing, "storage credential source is not configured")
					}
					if (disk.Driver == R2Disk || disk.Driver == CompatibleDisk) && c.Mode == credentials.Chain {
						return fault.New(fault.Invalid, "R2 and S3-compatible disks require explicit non-chain credentials")
					}
				}
				provider = credentials.ProviderFunc(func(context.Context) (credentials.Value, error) {
					return credentials.Value{}, fault.New(fault.Internal, "validation-only credentials")
				})
			}
			if _, err := disk.cloudConfig(provider); err != nil {
				return err
			}
		default:
			return fault.New(fault.Invalid, "unsupported storage driver")
		}
	}
	for _, name := range keys(s.Cache.Stores) {
		c := s.Cache.Stores[name]
		if c.Config.Namespace == (keyspace.Namespace{}) {
			c.Config.Namespace = s.Namespace
			c.Config.Namespace.Application += "." + string(name)
		}
		if c.Leases.Namespace == (keyspace.Namespace{}) {
			c.Leases.Namespace = c.Config.Namespace
		}
		if err := c.Config.Validate(); err != nil {
			return err
		}
		switch c.Driver {
		case NullCache:
		case MemoryCache:
			if err := c.Memory.Validate(); err != nil {
				return err
			}
		case RedisCache:
			if c.Redis == "" {
				c.Redis = s.Redis.Default
			}
			r, ok := s.Redis.Connections[c.Redis]
			if !ok {
				return fault.New(fault.Missing, "cache Redis connection is not configured")
			}
			if c.Config.MaxValueBytes > r.MaxValueBytes {
				return fault.New(fault.Invalid, "cache exceeds Redis value bound")
			}
		case FileCache:
			f := c.fileConfig(p.options.clock, p.options.logger)
			if err := f.Validate(); err != nil {
				return err
			}
		case PostgresCache:
			if c.Database == "" {
				c.Database = s.Database.Default
			}
			if _, ok := s.Database.Connections[c.Database]; !ok {
				return fault.New(fault.Missing, "cache database connection is not configured")
			}
			f := c.postgresConfig(p.options.clock, p.options.logger)
			if err := f.Validate(); err != nil {
				return err
			}
		default:
			return fault.New(fault.Invalid, "unsupported cache driver")
		}
		if c.Require.DistributedFills {
			if c.Driver != RedisCache {
				return fault.New(fault.Invalid, "distributed cache fills require Redis")
			}
			if err := c.Leases.Validate(); err != nil {
				return err
			}
			if c.Leases.Namespace != c.Config.Namespace {
				return fault.New(fault.Invalid, "cache fill namespace mismatch")
			}
		}
		if (c.Driver == FileCache || c.Driver == PostgresCache) && (c.Require.Tags || c.Require.Batches) {
			return fault.New(fault.Invalid, "persistent cache does not support requested tags or batches")
		}
		s.Cache.Stores[name] = c
	}
	return p.validateSupporting()
}
func (c CacheSettings) fileConfig(source clock.Clock, logger *slog.Logger) cachefile.Config {
	return cachefile.Config{Root: c.File.Root, MaxEntries: c.File.MaxEntries, MaxBytes: c.File.MaxBytes, MaxValueBytes: c.Config.MaxValueBytes, Sync: c.File.Sync, Clock: source, PruneInterval: c.File.PruneInterval, Logger: logger}
}

// postgresConfig uses PostgreSQL's own clock for expiry unless the application
// injected a non-system clock (deterministic tests), which it then keeps using.
func (c CacheSettings) postgresConfig(source clock.Clock, logger *slog.Logger) cachepg.Config {
	var expiry clock.Clock
	if _, system := source.(clock.System); !system {
		expiry = source
	}
	return cachepg.Config{Schema: c.Postgres.Schema, MaxEntries: c.Postgres.MaxEntries, MaxBytes: c.Postgres.MaxBytes, MaxValueBytes: c.Config.MaxValueBytes, Clock: expiry, PruneInterval: c.Postgres.PruneInterval, Logger: logger}
}

// Validate performs the same pure checks used by Configure, without preparing
// adapters. Custom credential providers must instead be supplied to Configure.
func (s Settings) Validate() error { _, err := Configure(s); return err }

// Plans retain configuration privately; formatting must not reflect through
// those private fields and bypass nested secret.String formatting.
func (Plan) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("infrastructure plan")) }
func (Plan) LogValue() slog.Value       { return slog.StringValue("infrastructure plan") }

// WithLogger borrows the containing application's structured logger.
func WithLogger(logger *slog.Logger) Option {
	return func(o *options) error {
		if logger == nil {
			return fault.New(fault.Invalid, "infrastructure logger is nil")
		}
		o.logger = logger
		return nil
	}
}

// WithMailDriver extends configured mailers with one borrowed driver. Its owner
// must outlive application shutdown. Built-in driver names cannot be replaced.
func WithMailDriver(name MailDriver, driver email.Driver) Option {
	return func(o *options) error {
		if err := namedservice.Validate(string(name)); err != nil {
			return err
		}
		if builtInMailDriver(name) || credential.IsNil(driver) {
			return fault.New(fault.Invalid, "custom mail driver requires a distinct name and instance")
		}
		if _, ok := o.mailDrivers[name]; ok {
			return fault.New(fault.Duplicate, "custom mail driver already registered")
		}
		if len(o.mailDrivers) >= namedservice.MaxEntries {
			return fault.New(fault.Invalid, "too many custom mail drivers")
		}
		o.mailDrivers[name] = driver
		return nil
	}
}
