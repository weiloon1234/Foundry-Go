package infrastructure

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
)

// PostgreSQLSettings is the serializable subset of postgres.Config. The explicit
// adapter API remains available for custom certificates/native customization.
type PostgreSQLSettings struct {
	Host                                            string
	Port                                            uint16
	Database, User                                  string
	Schema                                          string
	Password                                        secret.String
	TLS                                             postgres.TLSMode
	ApplicationName                                 string
	Pool                                            database.PoolConfig
	StatementCacheCapacity, MaxProtocolMessageBytes int
	StatementTimeout                                time.Duration
	LockTimeout                                     time.Duration
	IdleInTransactionSessionTimeout                 time.Duration
}

func PostgreSQLSettingsFromConfig(c postgres.Config) PostgreSQLSettings {
	return PostgreSQLSettings{Host: c.Host, Port: c.Port, Database: c.Database, User: c.User, Password: c.Password, TLS: c.TLS, ApplicationName: c.ApplicationName, Schema: c.Schema, Pool: c.Pool, StatementCacheCapacity: c.StatementCacheCapacity, MaxProtocolMessageBytes: c.MaxProtocolMessageBytes, StatementTimeout: c.StatementTimeout, LockTimeout: c.LockTimeout, IdleInTransactionSessionTimeout: c.IdleInTransactionSessionTimeout}
}
func (s PostgreSQLSettings) Config() postgres.Config {
	return postgres.Config{Host: s.Host, Port: s.Port, Database: s.Database, User: s.User, Password: s.Password, TLS: s.TLS, ApplicationName: s.ApplicationName, Schema: s.Schema, Pool: s.Pool, StatementCacheCapacity: s.StatementCacheCapacity, MaxProtocolMessageBytes: s.MaxProtocolMessageBytes, StatementTimeout: s.StatementTimeout, LockTimeout: s.LockTimeout, IdleInTransactionSessionTimeout: s.IdleInTransactionSessionTimeout}
}

// ConnectionSettings defines one logical primary/read topology.
//
//foundry:config
type ConnectionSettings struct {
	Primary        PostgreSQLSettings
	ReadEnabled    bool
	Read           PostgreSQLSettings
	MaxConnections int
	// SlowQueryThreshold logs statements at least this slow through the
	// application logger, without SQL text or arguments; zero disables it.
	SlowQueryThreshold time.Duration
	// StickyReadWindow routes reads to the primary for this long after a write
	// in a database.StickyReads request scope; zero disables it.
	StickyReadWindow time.Duration
}

func DefaultConnectionSettings() ConnectionSettings {
	c := postgres.DefaultConfig()
	s := PostgreSQLSettingsFromConfig(c)
	return ConnectionSettings{Primary: s, Read: s, MaxConnections: c.Pool.MaxOpen * 2}
}
func (s ConnectionSettings) Config() postgres.RoutingConfig {
	c := postgres.RoutingConfig{Primary: s.Primary.Config(), MaxConnections: s.MaxConnections, SlowQueryThreshold: s.SlowQueryThreshold, StickyReadWindow: s.StickyReadWindow}
	if s.ReadEnabled {
		c.Read = value.Set(s.Read.Config())
	}
	return c
}
func (s ConnectionSettings) Validate() error { return s.Config().Validate() }

// DatabaseConnections uses generated element keys, including nested durations
// and secrets. Each input entry starts with DefaultConnectionSettings.
type DatabaseConnections map[database.ConnectionName]ConnectionSettings

func (m *DatabaseConnections) UnmarshalText(data []byte) error {
	schema, err := ConnectionSettingsConfigSchema()
	if err != nil {
		return err
	}
	values, err := config.DecodeTable(string(data), schema, func(database.ConnectionName) ConnectionSettings { return DefaultConnectionSettings() }, nil)
	if err != nil {
		return err
	}
	*m = values
	return nil
}

type DatabaseSettings struct {
	Default        database.ConnectionName
	MaxConnections int
	Connections    DatabaseConnections `config:",secret"`
}

func DefaultDatabaseSettings() DatabaseSettings {
	return DatabaseSettings{Default: "default", MaxConnections: 128}
}
