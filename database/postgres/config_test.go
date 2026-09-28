package postgres

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
)

func explicitConfig() Config {
	c := DefaultConfig()
	c.Host, c.Database, c.User = "db.example", "app", "worker"
	c.Password = secret.New("explicit-password")
	return c
}

func TestURLDecodingIsExplicitStrictAndSecretSafe(t *testing.T) {
	input := &url.URL{Scheme: "postgresql", User: url.UserPassword("user@name", "pa:ss@/?#%"), Host: "[::1]:15432", Path: "/named database", RawQuery: "sslmode=disable&application_name=consumer"}
	config, err := ParseURL(secret.New(input.String()))
	if err != nil {
		t.Fatal(err)
	}
	if config.Host != "::1" || config.Port != 15432 || config.User != "user@name" || config.Password.Reveal() != "pa:ss@/?#%" || config.Database != "named database" || config.TLS != DisableTLS || config.ApplicationName != "consumer" {
		t.Fatal("URL components were not decoded exactly")
	}
	for _, text := range []string{"", "postgres://host/app", "mysql://u:p@host/app", "postgres://u:p@host/", "postgres://u:p@host/app#fragment", "postgres://u:p@host/app?sslmode=prefer", "postgres://u:p@host/app?service=private", "postgres://u:p@host/app?sslmode=disable&sslmode=require", "postgres://u:p@host:65536/app", "postgres://u:private-password@host:broken/app", "postgres://u:p@host1,host2/app"} {
		_, err := ParseURL(secret.New(text))
		if !errors.Is(err, fault.Invalid) {
			t.Fatalf("invalid URL accepted: %v", err)
		}
		for _, format := range []string{"%s", "%v", "%+v", "%#v"} {
			if strings.Contains(fmt.Sprintf(format, err), "private-password") {
				t.Fatal("URL error leaked credentials")
			}
		}
	}
	if _, err := ParseURL(secret.New("postgres://u:p@host:/app")); !errors.Is(err, fault.Invalid) {
		t.Fatal("empty explicit URL port accepted")
	}
}

func TestConfigurationMasksAmbientCredentialsAndSessionSettings(t *testing.T) {
	t.Setenv("PGSERVICE", "")
	for _, key := range []string{"PGHOST", "PGPORT", "PGDATABASE", "PGUSER", "PGPASSWORD", "PGPASSFILE", "PGAPPNAME", "PGCONNECT_TIMEOUT", "PGSSLMODE", "PGSSLKEY", "PGSSLCERT", "PGSSLSNI", "PGSSLROOTCERT", "PGSSLPASSWORD", "PGSSLNEGOTIATION", "PGTARGETSESSIONATTRS", "PGSERVICEFILE", "PGTZ", "PGOPTIONS", "PGMINPROTOCOLVERSION", "PGMAXPROTOCOLVERSION", "PGCHANNELBINDING", "PGREQUIREAUTH"} {
		t.Setenv(key, "ambient-invalid")
	}
	config := explicitConfig()
	config.Password = secret.String{} // an empty explicit password never loads a file
	parsed, err := connectionConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Host != config.Host || parsed.Port != config.Port || parsed.Database != config.Database || parsed.User != config.User || parsed.Password != "" || len(parsed.Fallbacks) != 0 {
		t.Fatal("ambient endpoint or credentials influenced adapter")
	}
	if !reflect.DeepEqual(parsed.RuntimeParams, map[string]string{"timezone": "UTC", "application_name": config.ApplicationName}) || parsed.ValidateConnect != nil || parsed.SSLNegotiation != "postgres" {
		t.Fatal("ambient session behavior leaked through")
	}
	if parsed.TLSConfig == nil || parsed.TLSConfig.InsecureSkipVerify || parsed.TLSConfig.ServerName != config.Host || parsed.TLSConfig.MinVersion < tls.VersionTLS12 {
		t.Fatal("default TLS peer verification missing")
	}
	t.Setenv("PGSERVICE", "ambient-service")
	if _, err := New(config); !errors.Is(err, fault.Invalid) {
		t.Fatal("ambient service file accepted")
	}
}

func TestConfigurationBoundsAndTLSSnapshots(t *testing.T) {
	t.Setenv("PGSERVICE", "")
	config := explicitConfig()
	config.TLSConfig = &tls.Config{ServerName: "verified.example"}
	parsed, err := connectionConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	config.TLSConfig.ServerName = "changed"
	if parsed.TLSConfig.ServerName != "verified.example" {
		t.Fatal("TLS configuration was not copied")
	}
	for _, invalid := range []func(*Config){func(c *Config) { c.Port = 0 }, func(c *Config) { c.Host = "/tmp" }, func(c *Config) { c.Pool.MaxOpen = 0 }, func(c *Config) { c.StatementCacheCapacity = -1 }, func(c *Config) { c.MaxProtocolMessageBytes = 0 }, func(c *Config) { c.TLSConfig.InsecureSkipVerify = true }, func(c *Config) { c.TLS = DisableTLS }, func(c *Config) { c.TLSConfig.MinVersion = tls.VersionTLS10 }} {
		copy := config.snapshot()
		invalid(&copy)
		if _, err := New(copy); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid config accepted")
		}
	}
	config.TLS, config.TLSConfig, config.StatementCacheCapacity = DisableTLS, nil, 0
	parsed, err = connectionConfig(config)
	if err != nil || parsed.TLSConfig != nil || parsed.DefaultQueryExecMode != pgx.QueryExecModeDescribeExec {
		t.Fatal("explicit disabled TLS/cache configuration not honored")
	}
}

func TestSQLStateClassificationPreservesCommitUncertainty(t *testing.T) {
	for _, item := range []struct {
		state    string
		code     database.Code
		rejected bool
	}{
		{"23505", database.UniqueViolation, true}, {"23503", database.ForeignKeyViolation, true}, {"23502", database.NotNullViolation, true}, {"23514", database.CheckViolation, true}, {"40001", database.SerializationFailure, true}, {"40P01", database.Deadlock, true}, {"40003", database.QueryFailed, false}, {"08007", database.Unavailable, false}, {"57014", database.Canceled, false}, {"28P01", database.Unavailable, false}, {"XX000", database.QueryFailed, false},
	} {
		original := &pgconn.PgError{Code: item.state, ConstraintName: "constraint_name", Message: "private statement", Detail: "private value"}
		result := classify(fmt.Errorf("wrapped: %w", original))
		if result.Code != item.code || result.CommitRejected != item.rejected || result.SQLState != item.state || result.Constraint != "constraint_name" {
			t.Fatalf("SQLSTATE %s: %+v", item.state, result)
		}
	}
	if !classify(pgx.ErrTxCommitRollback).CommitRejected || classify(errors.New("network response lost")).CommitRejected {
		t.Fatal("incorrect transaction completion classification")
	}
}
