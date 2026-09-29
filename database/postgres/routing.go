package postgres

import (
	"context"
	"slices"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/value"
)

// RoutingConfig owns typed primary/read endpoints and their combined process
// connection ceiling. Read omission keeps single-pool behavior. Credentials and
// TLS configuration retain Config's explicit ownership and redaction rules.
//
// SlowQueryThreshold, when positive, logs statements on either endpoint that
// take at least that long through the application logger (see
// database.WithSlowQueryThreshold); zero disables slow-statement logging.
//
// StickyReadWindow, when positive with a read endpoint, routes reads in a
// database.StickyReads request scope to the primary for that long after a
// write (see database.WithStickyReads).
type RoutingConfig struct {
	Primary            Config
	Read               value.Optional[Config]
	MaxConnections     int
	SlowQueryThreshold time.Duration
	StickyReadWindow   time.Duration
}

func (c RoutingConfig) Validate() error {
	if err := c.Primary.Validate(); err != nil {
		return err
	}
	if c.SlowQueryThreshold < 0 || c.StickyReadWindow < 0 {
		return fault.New(fault.Invalid, "PostgreSQL slow query threshold cannot be negative")
	}
	if c.MaxConnections <= 0 || c.Primary.Pool.MaxOpen > c.MaxConnections {
		return fault.New(fault.Invalid, "PostgreSQL primary pool exceeds the combined connection bound")
	}
	if read, configured := c.Read.Get(); configured {
		if err := read.Validate(); err != nil {
			return err
		}
		if read.Pool.MaxOpen > c.MaxConnections-c.Primary.Pool.MaxOpen {
			return fault.New(fault.Invalid, "PostgreSQL primary and read pools exceed the combined connection bound")
		}
	}
	return nil
}

func (c RoutingConfig) snapshot() RoutingConfig {
	c.Primary = c.Primary.snapshot()
	if read, configured := c.Read.Get(); configured {
		c.Read = value.Set(read.snapshot())
	}
	return c
}

func (c RoutingConfig) options(options []database.Option) []database.Option {
	result := slices.Clone(options)
	result = append(result, database.WithConnectionLimit(c.MaxConnections))
	if c.SlowQueryThreshold > 0 {
		result = append(result, database.WithSlowQueryThreshold(c.SlowQueryThreshold))
	}
	if c.StickyReadWindow > 0 {
		result = append(result, database.WithStickyReads(c.StickyReadWindow))
	}
	if read, configured := c.Read.Get(); configured {
		result = append(result, database.WithReadPool(func() (database.Adapter, error) { return New(read) }, read.Pool, c.MaxConnections))
	}
	return result
}

// OpenRouted verifies both configured endpoints before returning an owned DB.
// A configured replica startup failure closes both pools and returns no DB.
func OpenRouted(ctx context.Context, config RoutingConfig, options ...database.Option) (*database.DB, error) {
	config = config.snapshot()
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return Open(ctx, config.Primary, config.options(options)...)
}

// RoutedModule preserves the ordinary *database.DB service key, model observers
// and application clock. Both pools share the module's startup and shutdown.
// Construction is pure; dependency availability is checked during boot.
func RoutedModule(name foundation.ProviderID, key foundation.Key[*database.DB], config RoutingConfig, options ...database.Option) foundation.Module {
	config = config.snapshot()
	return database.Module(name, key, func() (database.Adapter, error) {
		if err := config.Validate(); err != nil {
			return database.Adapter{}, err
		}
		return New(config.Primary)
	}, config.Primary.Pool, config.options(options)...)
}
