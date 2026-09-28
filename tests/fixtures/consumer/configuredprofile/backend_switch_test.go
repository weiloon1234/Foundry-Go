package configuredprofile

import (
	"context"
	"crypto/rand"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/cloud/credentials"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/redis"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/storage"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCacheDriverSwitchKeepsTypedConsumer(t *testing.T) {
	for _, driver := range []infrastructure.CacheDriver{infrastructure.MemoryCache, infrastructure.FileCache, infrastructure.PostgresCache, infrastructure.RedisCache} {
		t.Run(string(driver), func(t *testing.T) {
			s := Defaults()
			s.Services.Namespace.Application = "switch-" + rand.Text()
			var migrationDB *database.DB
			var schema string
			if driver == infrastructure.PostgresCache {
				migrationDB = pgtest.Open(t)
				schema = pgtest.Namespace(t, migrationDB)
				connection := infrastructure.DefaultConnectionSettings()
				connection.Primary = infrastructure.PostgreSQLSettingsFromConfig(pgtest.Config(t))
				s.Services.Database.Connections = infrastructure.DatabaseConnections{"default": connection}
			}
			if driver == infrastructure.RedisCache {
				address := os.Getenv("FOUNDRY_TEST_REDIS_ADDR")
				if address == "" {
					if os.Getenv("FOUNDRY_TEST_REDIS_REQUIRED") == "1" {
						t.Fatal("required Redis endpoint missing")
					}
					t.Skip("native Redis is opt-in")
				}
				host, port, err := net.SplitHostPort(address)
				if err != nil {
					t.Fatal("invalid Redis test endpoint")
				}
				number, err := strconv.ParseUint(port, 10, 16)
				if err != nil {
					t.Fatal("invalid Redis test port")
				}
				connection := infrastructure.DefaultRedisConnectionSettings()
				connection.Host, connection.Port, connection.TLS = host, uint16(number), redis.DisableTLS
				s.Services.Redis.Connections = infrastructure.RedisConnections{"default": connection}
			}
			for name, item := range s.Services.Cache.Stores {
				item.Driver = driver
				if driver == infrastructure.FileCache {
					item.File.Root = t.TempDir()
				}
				if driver == infrastructure.PostgresCache {
					item.Postgres.Schema = schema
				}
				s.Services.Cache.Stores[name] = item
			}
			app, err := Build(t.Context(), s, quiet())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { stop(t, app) })
			if migrationDB != nil {
				targets := app.Migrations()
				if len(targets) != 1 || targets[0].Schema != schema {
					t.Fatal("shared cache migration was not deduplicated")
				}
				if err := migrationDB.Transaction(t.Context(), func(tx *database.Tx) error {
					if _, err := tx.Exec(t.Context(), `SET LOCAL search_path TO "`+schema+`", pg_temp`); err != nil {
						return err
					}
					for _, definition := range targets[0].Definitions {
						for _, statement := range definition.SQL {
							if _, err := tx.Exec(t.Context(), statement); err != nil {
								return err
							}
						}
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			if err := app.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			// Only configuration changed; the declaration, key/value types and calls do not.
			primary, err := app.Resources().Cache()
			if err != nil {
				t.Fatal(err)
			}
			named, err := NamedCache(app.Resources())
			if err != nil {
				t.Fatal(err)
			}
			first, err := Greetings.Bind(primary)
			if err != nil {
				t.Fatal(err)
			}
			second, err := Greetings.Bind(named)
			if err != nil {
				t.Fatal(err)
			}
			if err := first.Put(t.Context(), "message", "primary", cache.Forever()); err != nil {
				t.Fatal(err)
			}
			if _, hit, err := second.Get(t.Context(), "message"); err != nil || hit {
				t.Fatal("backend switch lost namespace isolation", err)
			}
			got, err := second.Remember(t.Context(), "message", cache.Forever(), func(context.Context) (string, error) { return "reports", nil })
			if err != nil || got != "reports" {
				t.Fatal("typed fill failed", err)
			}
			got, hit, err := first.Get(t.Context(), "message")
			if err != nil || !hit || got != "primary" {
				t.Fatal("typed read failed", err)
			}
			// Remove only the two values created by this test, never shared backend data.
			if _, err := first.Forget(t.Context(), "message"); err != nil {
				t.Fatal(err)
			}
			if _, err := second.Forget(t.Context(), "message"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Cloud cases execute real SDK signing with inert fixture credentials and never
// send a request to a cloud account. Provider I/O contracts have separate tests.
func TestStorageDriverSwitchKeepsNamedConsumerAndCredentialRouting(t *testing.T) {
	for _, driver := range []infrastructure.DiskDriver{infrastructure.LocalDisk, infrastructure.S3Disk, infrastructure.R2Disk} {
		t.Run(string(driver), func(t *testing.T) {
			s := Defaults()
			s.Services.Storage.Default = "primary"
			s.Services.Storage.Disks = make(infrastructure.Disks)
			s.Services.Credentials = make(infrastructure.CredentialSources)
			for _, name := range []storage.DiskID{"primary", "reports"} {
				item := infrastructure.DefaultDiskSettings()
				item.Driver = driver
				if driver == infrastructure.LocalDisk {
					item.Local.Root = t.TempDir()
				} else {
					item.Cloud.Bucket = "fixture-bucket"
					item.Cloud.Region = "us-east-1"
					item.Cloud.Namespace = string(name) + "/"
					item.Cloud.Credentials = credentials.Name(name)
					if driver == infrastructure.R2Disk {
						item.Cloud.Endpoint = "https://fixture.r2.cloudflarestorage.com"
					}
					s.Services.Credentials[credentials.Name(name)] = credentials.Settings{Mode: credentials.Static, AccessKey: secret.New("fixture-" + string(name)), SecretKey: secret.New("inert-test-secret")}
				}
				s.Services.Storage.Disks[name] = item
			}
			app, err := Build(t.Context(), s, quiet())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { stop(t, app) })
			if err := app.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			primary, err := app.Resources().Disk()
			if err != nil {
				t.Fatal(err)
			}
			alias, err := app.Resources().Storage.Disk("primary")
			if err != nil || primary != alias {
				t.Fatal("storage default is not its named instance", err)
			}
			other, err := app.Resources().Storage.Disk("reports")
			if err != nil || primary == other {
				t.Fatal("storage names share an owner", err)
			}
			key, err := storage.ParseKey("switch/value.txt")
			if err != nil {
				t.Fatal(err)
			}
			if driver == infrastructure.LocalDisk {
				if _, err := primary.PutBytes(t.Context(), key, []byte("local"), storage.PutOptions{}); err != nil {
					t.Fatal(err)
				}
				data, _, err := primary.ReadBytes(t.Context(), key, 64, storage.ReadOptions{})
				if err != nil || string(data) != "local" {
					t.Fatal("local configured read failed", err)
				}
				if exists, err := other.Exists(t.Context(), key); err != nil || exists {
					t.Fatal("local disks collided", err)
				}
			} else {
				for _, entry := range []struct {
					name string
					disk *storage.Disk
				}{{"primary", primary}, {"reports", other}} {
					link, err := entry.disk.TemporaryURL(t.Context(), key, storage.LinkOptions{ExpiresIn: time.Minute})
					if err != nil {
						t.Fatal("configured signer failed", err)
					}
					parsed, err := url.Parse(link.URL())
					if err != nil {
						t.Fatal("signer returned an invalid URL")
					}
					region := "us-east-1"
					if driver == infrastructure.R2Disk {
						region = "auto"
						if parsed.Hostname() != "fixture.r2.cloudflarestorage.com" {
							t.Fatal("R2 endpoint lost")
						}
					}
					if driver == infrastructure.S3Disk && !strings.HasSuffix(parsed.Hostname(), ".amazonaws.com") {
						t.Fatal("S3 endpoint lost")
					}
					credential := parsed.Query().Get("X-Amz-Credential")
					if !strings.HasPrefix(credential, "fixture-"+entry.name+"/") || !strings.HasSuffix(credential, "/"+region+"/s3/aws4_request") || !strings.HasSuffix(parsed.Path, "/"+entry.name+"/"+key.String()) {
						t.Fatal("configured credential, region or disk namespace was not preserved")
					}
				}
			}
		})
	}
}
