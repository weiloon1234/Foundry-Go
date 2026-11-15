package application_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/secret"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func generatedKey(t *testing.T, id encryption.KeyID) encryption.Key {
	t.Helper()
	key, err := encryption.GenerateKey(id)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// The key ring loads from secret files (NAME_FILE), including retired keys as
// named entries, and data encrypted under a previous key stays readable.
func TestApplicationKeyRingLoadsFromSecretFilesAndRetainsPreviousKeys(t *testing.T) {
	current, previous := generatedKey(t, "app_2026"), generatedKey(t, "app_2025")
	dir := t.TempDir()
	write := func(name string, value secret.String) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(value.Reveal()+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	environment := map[string]string{
		"APP__ENCRYPTION__KEY_ID":                       "app_2026",
		"APP__ENCRYPTION__KEY_FILE":                     write("current", current.Secret()),
		"APP__ENCRYPTION__PREVIOUS__APP_2025__KEY_FILE": write("previous", previous.Secret()),
	}
	schema, err := application.SettingsConfigSchema()
	if err != nil {
		t.Fatal(err)
	}
	s, report, err := schema.Load(settings(), config.Inputs[application.Settings]{Prefix: "APP", Environment: func(name string) (string, bool) {
		value, ok := environment[name]
		return value, ok
	}, Environ: func() []string {
		var entries []string
		for name, value := range environment {
			entries = append(entries, name+"="+value)
		}
		return entries
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range report.Entries() {
		if (entry.Name == "encryption.key" || strings.HasPrefix(entry.Name, "encryption.previous")) && !entry.Secret {
			t.Fatal("encryption key provenance is not marked secret", entry.Name)
		}
	}
	s.HTTP.Enabled = false
	app, err := application.New(s, quiet()).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(t, app) })
	keys, err := app.Resources().Encryption()
	if err != nil || keys.ActiveID() != "app_2026" {
		t.Fatal("application key ring was not configured", err)
	}
	binding, err := encryption.NewContext("fixture.notes", secret.New("note:1"))
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := encryption.NewKeyring("app_2025", previous)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := legacy.Encrypt(t.Context(), binding, secret.New("retained"))
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := keys.Reencrypt(t.Context(), binding, sealed)
	if err != nil || rotated.KeyID() != "app_2026" {
		t.Fatal("previous key was not retained for rotation", err)
	}
	if _, err := app.Resources().CookieEncrypter(); err != nil {
		t.Fatal("cookie encrypter did not use the application key ring", err)
	}
}

func TestApplicationKeyRingValidationAndMissingKeys(t *testing.T) {
	current := generatedKey(t, "app_2026")
	s := settings()
	s.HTTP.Enabled = false
	app, err := application.New(s, quiet()).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(t, app) })
	if _, err := app.Resources().Encryption(); !errors.Is(err, fault.Missing) {
		t.Fatal("unconfigured key ring was available", err)
	}
	for name, change := range map[string]func(*application.Settings){
		"material": func(s *application.Settings) { s.Encryption.KeyID, s.Encryption.Key = "app_2026", secret.New("short") },
		"no id":    func(s *application.Settings) { s.Encryption.Key = current.Secret() },
		"duplicate": func(s *application.Settings) {
			s.Encryption.KeyID, s.Encryption.Key = "app_2026", current.Secret()
			s.Encryption.Previous = application.EncryptionKeys{"app_2026": {Key: current.Secret()}}
		},
		"mfa": func(s *application.Settings) { s.Features.Auth.MFA.Enabled = true },
	} {
		s := settings()
		s.HTTP.Enabled = false
		change(&s)
		if _, err := application.New(s, quiet()).Build(t.Context()); err == nil {
			t.Fatal("invalid encryption configuration was accepted:", name)
		}
	}
}

// The configured MFA store uses the application key ring, and `mfa reencrypt`
// runs as an ordinary operator command without any user's password.
func TestConfiguredMFAStoreAndReencryptCommand(t *testing.T) {
	migrationDB := pgtest.Open(t)
	schema := pgtest.Namespace(t, migrationDB)
	s := settings()
	s.HTTP.Enabled = false
	connection := infrastructure.DefaultConnectionSettings()
	connection.Primary = infrastructure.PostgreSQLSettingsFromConfig(pgtest.Config(t))
	s.Services.Database.Connections = infrastructure.DatabaseConnections{"default": connection}
	s.Features.Auth.MFA.Enabled = true
	s.Features.Auth.MFA.Schema = schema
	s.Encryption.KeyID, s.Encryption.Key = "app_2026", generatedKey(t, "app_2026").Secret()
	declaration, err := application.MFACommand()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := cli.New(declaration)
	if err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	invocation, err := registry.Parse([]string{"mfa", "reencrypt", "--batch", "16"}, &output)
	if err != nil {
		t.Fatal(err)
	}
	commands := cli.Module("fixture.mfa.cli", foundation.NewKey[*cli.Registry]("fixture.mfa.commands"), registry, invocation, cli.Streams{In: strings.NewReader(""), Out: &output, Err: &output}, application.MFAProvider)
	app, err := application.New(s, quiet()).Register(commands).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := migrationDB.Transaction(t.Context(), func(tx *database.Tx) error {
		if _, err := tx.Exec(t.Context(), `SET LOCAL search_path TO "`+schema+`", pg_temp`); err != nil {
			return err
		}
		for _, target := range app.Migrations() {
			for _, definition := range target.Definitions {
				for _, sql := range definition.SQL {
					if _, err := tx.Exec(t.Context(), sql); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := app.Run(t.Context(), foundation.CLI); err != nil {
		t.Fatal(err, output.String())
	}
	if !strings.Contains(output.String(), "reencrypted=0 changed=0 failed=0 complete=true") {
		t.Fatal("unexpected command output", output.String())
	}
}
