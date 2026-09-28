package redis

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
)

func explicitConfig() Config {
	c := DefaultConfig()
	c.Host = "127.0.0.1"
	c.TLS = DisableTLS
	return c
}
func TestConfigHasNoAmbientSettingsAndSnapshotsTLS(t *testing.T) {
	t.Setenv("REDIS_URL", "redis://wrong:credential@invalid:9999/8")
	c := explicitConfig()
	c.Password = secret.New("private-password")
	client, err := Prepare(c)
	if err != nil {
		t.Fatal(err)
	}
	if client.raw != nil || client.Stats().Ready || client.Stats().Open != 0 {
		t.Fatal("Prepare acquired resources")
	}
	options := client.config.options()
	if options.Addr != "127.0.0.1:6379" || options.DB != 0 || options.Password != "private-password" || options.MaxRetries != -1 || options.DialerRetries != 1 || !options.ContextTimeoutEnabled || options.MaxActiveConns != c.MaxConnections {
		t.Fatal("configuration changed")
	}
	if strings.Contains(fmt.Sprint(c), "private-password") {
		t.Fatal("credential exposed")
	}
	c.TLS = VerifyTLS
	c.TLSConfig = &tls.Config{RootCAs: x509.NewCertPool(), ServerName: "redis.example"}
	client, err = Prepare(c)
	if err != nil {
		t.Fatal(err)
	}
	c.TLSConfig.ServerName = "changed"
	if client.config.TLSConfig == c.TLSConfig || client.config.TLSConfig.RootCAs == c.TLSConfig.RootCAs || client.config.options().TLSConfig.ServerName != "redis.example" {
		t.Fatal("TLS configuration retained")
	}
	if err := client.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := client.Start(t.Context()); !errors.Is(err, fault.Closed) {
		t.Fatal(err)
	}
}
func TestInvalidRedisConfig(t *testing.T) {
	for name, change := range map[string]func(*Config){
		"host": func(c *Config) { c.Host = "" }, "port": func(c *Config) { c.Port = 0 }, "database": func(c *Config) { c.Database = -1 },
		"connections": func(c *Config) { c.MaxConnections = 0 }, "operations": func(c *Config) { c.MaxOperations = 0 },
		"timeout": func(c *Config) { c.OperationTimeout = 0 }, "payload": func(c *Config) { c.MaxValueBytes = 0 },
		"tls":         func(c *Config) { c.TLS = VerifyTLS; c.TLSConfig = &tls.Config{InsecureSkipVerify: true} },
		"tls-version": func(c *Config) { c.TLS = VerifyTLS; c.TLSConfig = &tls.Config{MaxVersion: tls.VersionTLS11} },
	} {
		t.Run(name, func(t *testing.T) {
			c := explicitConfig()
			change(&c)
			if _, err := Prepare(c); !errors.Is(err, fault.Invalid) {
				t.Fatal(err)
			}
		})
	}
}
func TestTTLConversionRoundsUpWithoutOverflow(t *testing.T) {
	for _, tc := range []struct {
		ttl  cache.TTL
		want string
	}{{cache.Forever(), "0"}, {cache.For(time.Nanosecond), "1"}, {cache.For(time.Millisecond), "1"}, {cache.For(time.Millisecond + 1), "2"}, {cache.For(time.Duration(math.MaxInt64)), "9223372036855"}} {
		got, err := milliseconds(tc.ttl)
		if err != nil || got != tc.want {
			t.Fatal(got, tc.want, err)
		}
	}
	for _, ttl := range []cache.TTL{{}, cache.For(-1)} {
		if _, err := milliseconds(ttl); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
}
func TestPreparedAndZeroClientsRejectUse(t *testing.T) {
	var zero Client
	if err := zero.Ping(t.Context()); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	client, err := Prepare(explicitConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Ping(t.Context()); !errors.Is(err, fault.Conflict) {
		t.Fatal(err)
	}
	if err := client.Start(nil); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := client.Start(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if client.started {
		t.Fatal("canceled call consumed start")
	}
	if err := client.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	<-client.Done()
	if err := client.Close(ctx); err != nil {
		t.Fatal(err)
	}
}
