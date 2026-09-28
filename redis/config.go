// Package redis owns an explicitly configured Redis connection pool. Feature
// packages consume its focused capabilities; application code uses typed handles.
package redis

import (
	"crypto/tls"
	"net"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	driver "github.com/redis/go-redis/v9"
	"github.com/redis/go-redis/v9/maintnotifications"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
)

// TLSMode selects verified TLS or explicitly unencrypted transport. There is no
// automatic downgrade or fallback to a different endpoint.
type TLSMode string

const (
	VerifyTLS  TLSMode = "verify-full"
	DisableTLS TLSMode = "disable"
)

// Config addresses one standalone Redis server. It performs no environment or
// credential-file discovery. Cluster and Sentinel routing are not implemented.
// TLS certificate/key material must remain immutable after Prepare.
type Config struct {
	Host           string
	Port           uint16
	Database       int
	User           string
	Password       secret.String
	TLS            TLSMode
	TLSConfig      *tls.Config
	MaxConnections int
	MaxOperations  int
	// MaxSubscriptions separately bounds dedicated pub/sub connections.
	MaxSubscriptions int
	ConnectTimeout   time.Duration
	OperationTimeout time.Duration
	PoolTimeout      time.Duration
	MaxIdleTime      time.Duration
	MaxLifetime      time.Duration
	// MaxValueBytes bounds cache writes and server-side reads before GET returns
	// the value. It is not a bound on an untrusted server's RESP parser allocations.
	MaxValueBytes int
}

func DefaultConfig() Config {
	return Config{Port: 6379, TLS: VerifyTLS, MaxConnections: 16, MaxOperations: 128, MaxSubscriptions: 64,
		ConnectTimeout: 5 * time.Second, OperationTimeout: 5 * time.Second, PoolTimeout: 5 * time.Second,
		MaxIdleTime: 5 * time.Minute, MaxLifetime: 30 * time.Minute, MaxValueBytes: 1 << 20}
}
func (c Config) Validate() error {
	if c.Host == "" || strings.ContainsAny(c.Host, ",/\\ \t\r\n") || !validText(c.Host) || c.Port == 0 || c.Database < 0 || c.Database > int(^uint32(0)>>1) || !validText(c.User) || !validText(c.Password.Reveal()) {
		return fault.New(fault.Invalid, "Redis needs an explicit TCP endpoint and valid credentials/database")
	}
	if c.MaxConnections <= 0 || c.MaxConnections > 65536 || c.MaxOperations < c.MaxConnections || c.MaxOperations > 1<<20 || c.MaxSubscriptions <= 0 || c.MaxSubscriptions > 65536 || c.ConnectTimeout <= 0 || c.OperationTimeout <= 0 || c.PoolTimeout <= 0 || c.MaxIdleTime <= 0 || c.MaxLifetime <= 0 || c.MaxValueBytes <= 0 || c.MaxValueBytes > 512<<20 {
		return fault.New(fault.Invalid, "invalid Redis connection, operation or payload bounds")
	}
	switch c.TLS {
	case VerifyTLS:
		if c.TLSConfig != nil {
			v := c.TLSConfig
			if v.InsecureSkipVerify || v.MinVersion != 0 && v.MinVersion < tls.VersionTLS12 || v.MaxVersion != 0 && v.MaxVersion < max(v.MinVersion, tls.VersionTLS12) {
				return fault.New(fault.Invalid, "Redis TLS requires verified peers and TLS 1.2 or newer")
			}
		}
	case DisableTLS:
		if c.TLSConfig != nil {
			return fault.New(fault.Invalid, "disabled Redis TLS cannot configure TLS")
		}
	default:
		return fault.New(fault.Invalid, "unsupported Redis TLS mode")
	}
	return nil
}
func validText(s string) bool { return utf8.ValidString(s) && !strings.ContainsRune(s, 0) }
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
func (c Config) options() *driver.Options {
	var transport *tls.Config
	if c.TLS == VerifyTLS {
		transport = &tls.Config{MinVersion: tls.VersionTLS12}
		if c.TLSConfig != nil {
			transport = c.TLSConfig.Clone()
			transport.MinVersion = max(transport.MinVersion, tls.VersionTLS12)
		}
		if transport.ServerName == "" {
			transport.ServerName = c.Host
		}
	}
	return &driver.Options{Network: "tcp", Addr: net.JoinHostPort(c.Host, strconv.Itoa(int(c.Port))),
		Username: c.User, Password: c.Password.Reveal(), DB: c.Database, TLSConfig: transport,
		Protocol: 2, MaxRetries: -1, DialerRetries: 1, ContextTimeoutEnabled: true,
		DialTimeout: c.ConnectTimeout, ReadTimeout: c.OperationTimeout, WriteTimeout: c.OperationTimeout,
		PoolTimeout: c.PoolTimeout, PoolSize: c.MaxConnections, MaxActiveConns: c.MaxConnections,
		MaxIdleConns: c.MaxConnections, MaxConcurrentDials: c.MaxConnections,
		ConnMaxIdleTime: c.MaxIdleTime, ConnMaxLifetime: c.MaxLifetime,
		DisableIdentity: true, MaintNotificationsConfig: &maintnotifications.Config{Mode: maintnotifications.ModeDisabled}}
}
