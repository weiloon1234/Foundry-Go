package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
)

type connection struct {
	Host     string
	Port     int
	Password secret.String
}

type connections map[string]connection

func (c *connections) UnmarshalText(data []byte) error {
	schema, err := config.New(config.String("host", func(s *connection) *string { return &s.Host }), config.Int("port", func(s *connection) *int { return &s.Port }), config.Secret("password", func(s *connection) *secret.String { return &s.Password }))
	if err != nil {
		return err
	}
	values, err := config.DecodeTable(string(data), schema, func(string) connection { return connection{Port: 5432} }, nil)
	if err != nil {
		return err
	}
	*c = values
	return nil
}

type deployment struct {
	Name        string
	Connections connections
}

func deploymentSchema(t *testing.T) *config.Schema[deployment] {
	t.Helper()
	schema, err := config.New(
		config.String("app.name", func(s *deployment) *string { return &s.Name }),
		config.Text[deployment, connections]("database.connections", func(s *deployment) *connections { return &s.Connections }).Sensitive(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func environment(values map[string]string) (config.Lookup, func() []string) {
	lookup := func(name string) (string, bool) { value, ok := values[name]; return value, ok }
	return lookup, func() []string {
		pairs := make([]string, 0, len(values))
		for name, value := range values {
			pairs = append(pairs, name+"="+value)
		}
		return pairs
	}
}

func secretFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFileVariantsReadMountedSecretsWithoutFormattingThem(t *testing.T) {
	s := schema(t)
	lookup, _ := environment(map[string]string{"APP__DATABASE__PASSWORD_FILE": secretFile(t, "mounted-private\n"), "APP__APP__NAME_FILE": secretFile(t, "line\r\n")})
	values, report, err := s.Load(settings{}, config.Inputs[settings]{Prefix: "APP", Environment: lookup})
	if err != nil || values.Password.Reveal() != "mounted-private" || values.Name != "line" {
		t.Fatal("file variant was not read or trimmed", err, values.Name)
	}
	for _, entry := range report.Entries() {
		if entry.Name == "database.password" && entry.Source != "environment-file:APP__DATABASE__PASSWORD" {
			t.Fatal("file provenance missing", entry)
		}
	}
	for _, inputs := range []map[string]string{
		{"APP__DATABASE__PASSWORD": "direct-private", "APP__DATABASE__PASSWORD_FILE": secretFile(t, "file-private")},
		{"APP__DATABASE__PASSWORD_FILE": filepath.Join(t.TempDir(), "missing")},
		{"APP__DATABASE__PASSWORD_FILE": t.TempDir()},
		{"APP__DATABASE__PASSWORD_FILE": ""},
	} {
		lookup, _ := environment(inputs)
		_, _, err := s.Load(settings{}, config.Inputs[settings]{Prefix: "APP", Environment: lookup})
		if !errors.Is(err, fault.Invalid) || strings.Contains(err.Error(), "private") {
			t.Fatal("invalid file variant accepted or leaked", err)
		}
	}
	collision := func(s *settings) *string { return &s.Name }
	if _, err := config.New(config.String("tls.cert", collision), config.String("tls.cert_file", collision)); !errors.Is(err, fault.Duplicate) {
		t.Fatal("ambiguous _FILE environment names accepted", err)
	}
}

func TestNamedCollectionEntryOverridesMergeIntoSuppliedTable(t *testing.T) {
	s := deploymentSchema(t)
	file := config.Values{Name: "file", Data: map[string]string{"database.connections": `{"main":{"host":"db.internal"},"read":{"host":"replica.internal","port":6432}}`}}
	lookup, environ := environment(map[string]string{
		"APP__DATABASE__CONNECTIONS__MAIN__PASSWORD":      "entry-private",
		"APP__DATABASE__CONNECTIONS__READ__PASSWORD_FILE": secretFile(t, "file-private\n"),
		"APP__DATABASE__CONNECTIONS__READ__PORT":          "7000",
		"OTHER__DATABASE__CONNECTIONS__MAIN__HOST":        "ignored",
	})
	values, report, err := s.Load(deployment{}, config.Inputs[deployment]{Files: []config.Values{file}, Prefix: "APP", Environment: lookup, Environ: environ})
	if err != nil {
		t.Fatal(err)
	}
	main, read := values.Connections["main"], values.Connections["read"]
	if len(values.Connections) != 2 || main.Host != "db.internal" || main.Port != 5432 || main.Password.Reveal() != "entry-private" || read.Host != "replica.internal" || read.Port != 7000 || read.Password.Reveal() != "file-private" {
		t.Fatal("entry overrides did not merge field by field", values.Connections)
	}
	for _, entry := range report.Entries() {
		if entry.Name == "database.connections" && (entry.Source != "environment:APP__DATABASE__CONNECTIONS__*" || !entry.Secret) {
			t.Fatal("entry override provenance missing", entry)
		}
	}
	if values, _, err := s.Load(deployment{}, config.Inputs[deployment]{Files: []config.Values{file}, Prefix: "APP", Environment: lookup}); err != nil || values.Connections["main"].Password.Reveal() != "" {
		t.Fatal("entry overrides applied without an explicit environment listing", err)
	}
	lookup, environ = environment(map[string]string{"APP__DATABASE__CONNECTIONS__REPORTS__HOST": "reports.internal"})
	values, _, err = s.Load(deployment{}, config.Inputs[deployment]{Prefix: "APP", Environment: lookup, Environ: environ})
	if err != nil || values.Connections["reports"].Host != "reports.internal" || values.Connections["reports"].Port != 5432 {
		t.Fatal("entry override could not create an entry in an empty collection", err, values.Connections)
	}
	for _, inputs := range []map[string]string{
		{"APP__DATABASE__CONNECTIONS__MAIN__UNKNOWN": "private-value"},
		{"APP__DATABASE__CONNECTIONS__MAIN": "private-value"},
		{"APP__DATABASE__CONNECTIONS__MAIN__PORT": "not-a-port"},
		{"APP__DATABASE__CONNECTIONS__MAIN__HOST__NESTED": "private-value"},
		{"APP__DATABASE__CONNECTIONS___MAIN__HOST": "private-value"},
		{"APP__DATABASE__CONNECTIONS__MAIN__PASSWORD": "private-value", "APP__DATABASE__CONNECTIONS__MAIN__PASSWORD_FILE": secretFile(t, "private-file")},
	} {
		lookup, environ := environment(inputs)
		_, _, err := s.Load(deployment{}, config.Inputs[deployment]{Files: []config.Values{file}, Prefix: "APP", Environment: lookup, Environ: environ})
		if !errors.Is(err, fault.Invalid) || strings.Contains(err.Error(), "private") {
			t.Fatal("invalid entry override accepted or leaked", inputs, err)
		}
	}
	defaults := deployment{Connections: connections{"main": {Host: "go-default", Port: 1}}}
	lookup, environ = environment(map[string]string{"APP__DATABASE__CONNECTIONS__MAIN__HOST": "override"})
	if _, _, err := s.Load(defaults, config.Inputs[deployment]{Prefix: "APP", Environment: lookup, Environ: environ}); !errors.Is(err, fault.Invalid) {
		t.Fatal("entry override silently replaced a Go default collection", err)
	}
}
