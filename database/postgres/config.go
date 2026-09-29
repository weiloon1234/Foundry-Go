// Package postgres adapts pgx to Foundry's database contracts. Configuration is
// explicit and construction performs no connection or schema migration.
package postgres

import (
	"crypto/tls"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
	"github.com/weiloon1234/Foundry-Go/secret"
)

// TLSMode selects a single transport policy without plaintext fallback.
type TLSMode string

const (
	VerifyFull TLSMode = "verify-full"
	RequireTLS TLSMode = "require" // Encrypts transport; does not verify server identity.
	DisableTLS TLSMode = "disable"
)

// Config owns explicit endpoint, credential and resource settings. TLSConfig is
// an optional standard TLS configuration; certificate/key material must remain
// immutable after handing it to the adapter, as required by crypto/tls.
type Config struct {
	Host            string
	Port            uint16
	Database        string
	User            string
	Password        secret.String
	TLS             TLSMode
	TLSConfig       *tls.Config
	ApplicationName string
	// Schema selects one existing application schema with no public fallback.
	// Empty preserves the server default. Scoped pools restore this on checkout.
	Schema                  string
	Pool                    database.PoolConfig
	StatementCacheCapacity  int
	MaxProtocolMessageBytes int
	// Server-side session limits, set once per connection in whole
	// milliseconds. Zero keeps the server default. StatementTimeout cancels a
	// statement (QueryCanceled), LockTimeout bounds lock waits
	// (LockNotAvailable) and IdleInTransactionSessionTimeout terminates a
	// session left idle inside an open transaction.
	StatementTimeout                time.Duration
	LockTimeout                     time.Duration
	IdleInTransactionSessionTimeout time.Duration
}

// maxSessionTimeout is PostgreSQL's integer millisecond GUC upper bound.
const maxSessionTimeout = time.Duration(1<<31-1) * time.Millisecond

// DefaultConfig requires an explicit endpoint and user. It verifies TLS peers,
// sets UTC session time, and bounds both statement caching and protocol messages.
func DefaultConfig() Config {
	return Config{Port: 5432, TLS: VerifyFull, ApplicationName: "foundry-go", Pool: database.DefaultPoolConfig(), StatementCacheCapacity: 512, MaxProtocolMessageBytes: 64 << 20}
}

func validText(value string) bool { return utf8.ValidString(value) && !strings.ContainsRune(value, 0) }

func (c Config) Validate() error {
	if c.Schema != "" && (!sqlname.Valid(c.Schema) || strings.HasPrefix(strings.ToLower(c.Schema), "pg_") || strings.EqualFold(c.Schema, "information_schema")) {
		return fault.New(fault.Invalid, "PostgreSQL scope requires a non-system schema identifier")
	}
	if c.Host == "" || strings.TrimSpace(c.Host) != c.Host || strings.ContainsAny(c.Host, ",/\\ \t\r\n") || !validText(c.Host) || c.Port == 0 || c.Database == "" || c.User == "" || !validText(c.Database) || !validText(c.User) || !validText(c.Password.Reveal()) || !validText(c.ApplicationName) {
		return fault.New(fault.Invalid, "PostgreSQL needs an explicit TCP host, port, database and user with valid text values")
	}
	if c.StatementCacheCapacity < 0 || c.StatementCacheCapacity > 65536 || c.MaxProtocolMessageBytes < 1024 || c.MaxProtocolMessageBytes > 1<<30 {
		return fault.New(fault.Invalid, "invalid PostgreSQL statement cache or protocol message bound")
	}
	for _, limit := range []time.Duration{c.StatementTimeout, c.LockTimeout, c.IdleInTransactionSessionTimeout} {
		if limit < 0 || limit%time.Millisecond != 0 || limit > maxSessionTimeout {
			return fault.New(fault.Invalid, "PostgreSQL session timeouts must be whole non-negative milliseconds within the server bound")
		}
	}
	switch c.TLS {
	case VerifyFull, RequireTLS:
		if c.TLSConfig != nil {
			if c.TLSConfig.MinVersion != 0 && c.TLSConfig.MinVersion < tls.VersionTLS12 {
				return fault.New(fault.Invalid, "PostgreSQL TLS requires TLS 1.2 or newer")
			}
			minimum := max(c.TLSConfig.MinVersion, tls.VersionTLS12)
			if c.TLSConfig.MaxVersion != 0 && c.TLSConfig.MaxVersion < minimum {
				return fault.New(fault.Invalid, "PostgreSQL TLS version bounds conflict")
			}
			if c.TLS == VerifyFull && c.TLSConfig.InsecureSkipVerify {
				return fault.New(fault.Invalid, "verify-full cannot disable TLS peer verification")
			}
		}
	case DisableTLS:
		if c.TLSConfig != nil {
			return fault.New(fault.Invalid, "disabled PostgreSQL TLS cannot also configure TLS")
		}
	default:
		return fault.New(fault.Invalid, "unsupported PostgreSQL TLS mode")
	}
	return c.Pool.Validate()
}

// ParseURL decodes an explicit postgres/postgresql URL without libpq environment
// fallback or credential-file access. It accepts only sslmode and application_name
// query keys. Other settings use typed Config fields. Unknown/repeated keys fail.
func ParseURL(value secret.String) (Config, error) {
	parsed, err := url.Parse(value.Reveal())
	if err != nil {
		return Config{}, fault.Wrap(fault.Invalid, "invalid PostgreSQL URL", err)
	}
	if (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.Opaque != "" || parsed.Fragment != "" || parsed.ForceQuery || parsed.User == nil || parsed.Hostname() == "" || !strings.HasPrefix(parsed.Path, "/") {
		return Config{}, fault.New(fault.Invalid, "PostgreSQL URL needs an explicit host, user and database")
	}
	config := DefaultConfig()
	if strings.HasSuffix(parsed.Host, ":") {
		return Config{}, fault.New(fault.Invalid, "PostgreSQL URL has an empty explicit port")
	}
	config.Host = parsed.Hostname()
	config.Database = strings.TrimPrefix(parsed.Path, "/")
	config.User = parsed.User.Username()
	password, _ := parsed.User.Password()
	config.Password = secret.New(password)
	if port := parsed.Port(); port != "" {
		number, err := strconv.ParseUint(port, 10, 16)
		if err != nil || number == 0 {
			return Config{}, fault.New(fault.Invalid, "invalid PostgreSQL URL port")
		}
		config.Port = uint16(number)
	}
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return Config{}, fault.Wrap(fault.Invalid, "invalid PostgreSQL URL query", err)
	}
	for key, values := range query {
		if len(values) != 1 {
			return Config{}, fault.New(fault.Invalid, "PostgreSQL URL repeats a query setting")
		}
		switch key {
		case "sslmode":
			config.TLS = TLSMode(values[0])
		case "application_name":
			config.ApplicationName = values[0]
		default:
			return Config{}, fault.New(fault.Invalid, "unsupported PostgreSQL URL query setting; use typed configuration")
		}
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (c Config) snapshot() Config {
	if c.TLSConfig != nil {
		c.TLSConfig = c.TLSConfig.Clone()
		if c.TLSConfig.RootCAs != nil {
			c.TLSConfig.RootCAs = c.TLSConfig.RootCAs.Clone()
		}
		if c.TLSConfig.ClientCAs != nil {
			c.TLSConfig.ClientCAs = c.TLSConfig.ClientCAs.Clone()
		}
	}
	return c
}
