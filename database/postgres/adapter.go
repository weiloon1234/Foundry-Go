package postgres

import (
	"context"
	"crypto/tls"
	"net"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

// New constructs an application-owned adapter without connecting. Credential and
// session settings come only from Config. The pgx parser's ambient PGSERVICE
// setting is rejected because upstream resolves service files before returning.
func New(config Config) (database.Adapter, error) {
	parsed, err := connectionConfig(config)
	if err != nil {
		return database.Adapter{}, err
	}
	return database.Adapter{Connector: stdlib.GetConnector(*parsed, schemaOptions(config.Schema)...), Classify: classify}, nil
}

func Open(ctx context.Context, config Config, options ...database.Option) (*database.DB, error) {
	adapter, err := New(config)
	if err != nil {
		return nil, err
	}
	return database.Open(ctx, adapter, config.Pool, options...)
}

// Module binds a fresh prepared PostgreSQL pool per application. Boot verifies
// connectivity and shutdown drains its owners through database.Module.
func Module(name foundation.ProviderID, key foundation.Key[*database.DB], config Config, options ...database.Option) foundation.Module {
	config = config.snapshot()
	return database.Module(name, key, func() (database.Adapter, error) { return New(config) }, config.Pool, options...)
}

func connectionConfig(config Config) (*pgx.ConnConfig, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if os.Getenv("PGSERVICE") != "" {
		return nil, fault.New(fault.Invalid, "ambient PGSERVICE is unsupported; use explicit Foundry PostgreSQL configuration")
	}
	// pgx requires a configuration produced by ParseConfig. This credential-free
	// template masks every environment setting that can affect parsing. TLS and
	// password files are disabled; all real fields are assigned below. servicefile
	// stays empty so a concurrent change to PGSERVICE fails without reading a file.
	const template = "host=127.0.0.1 port=5432 user=foundry database=foundry password=unused passfile='' servicefile='' sslmode=disable sslrootcert='' sslcert='' sslkey='' sslpassword='' sslsni=1 sslnegotiation=postgres connect_timeout=0 target_session_attrs=any min_protocol_version=3.0 max_protocol_version=3.0 channel_binding=prefer require_auth=''"
	parsed, err := pgx.ParseConfig(template)
	if err != nil {
		return nil, fault.Wrap(fault.Invalid, "cannot construct explicit PostgreSQL driver configuration", err)
	}
	config = config.snapshot()
	parsed.Host, parsed.Port = config.Host, config.Port
	parsed.Database, parsed.User, parsed.Password = config.Database, config.User, config.Password.Reveal()
	parsed.ConnectTimeout = config.Pool.ConnectTimeout
	dialer := &net.Dialer{Timeout: config.Pool.ConnectTimeout}
	parsed.DialFunc = dialer.DialContext
	parsed.Fallbacks = nil
	parsed.RuntimeParams = map[string]string{"application_name": config.ApplicationName, "timezone": "UTC"}
	if config.Schema != "" {
		parsed.RuntimeParams["search_path"] = `"` + config.Schema + `"`
	}
	parsed.SSLNegotiation = "postgres"
	parsed.MaxProtocolMessageBodyLen = config.MaxProtocolMessageBytes
	parsed.StatementCacheCapacity = config.StatementCacheCapacity
	parsed.DescriptionCacheCapacity = config.StatementCacheCapacity
	if config.StatementCacheCapacity == 0 {
		parsed.DefaultQueryExecMode = pgx.QueryExecModeDescribeExec
	}
	if config.TLS != DisableTLS {
		parsed.TLSConfig = config.TLSConfig
		if parsed.TLSConfig == nil {
			parsed.TLSConfig = &tls.Config{}
		}
		parsed.TLSConfig.MinVersion = max(parsed.TLSConfig.MinVersion, tls.VersionTLS12)
		if parsed.TLSConfig.ServerName == "" {
			parsed.TLSConfig.ServerName = config.Host
		}
		parsed.TLSConfig.InsecureSkipVerify = config.TLS == RequireTLS
	}
	return parsed, nil
}
