package vault_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"foundry.test/consumer/vault"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/encrypted"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/fault"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

// pool opens the retained test schema, optionally with a field key ring.
func pool(t *testing.T, schema string, options ...database.Option) *database.DB {
	t.Helper()
	config := pgtest.Config(t)
	config.Schema = schema
	db, err := postgres.Open(t.Context(), config, options...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := db.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return db
}

func keyring(t *testing.T, active encryption.KeyID, keys ...encryption.Key) *encryption.Keyring {
	t.Helper()
	ring, err := encryption.NewKeyring(active, keys...)
	if err != nil {
		t.Fatal(err)
	}
	return ring
}

func stored(t *testing.T, db *database.DB, id string) (token string, settings *string) {
	t.Helper()
	if err := database.ScanOne(t.Context(), db, "SELECT token, settings FROM vault_entries WHERE id = $1", []any{id}, &token, &settings); err != nil {
		t.Fatal(err)
	}
	return token, settings
}

func TestEncryptedFieldsStoreBoundEnvelopesAndHydratePlaintext(t *testing.T) {
	schema := pgtest.Namespace(t, pgtest.Open(t))
	first, err := encryption.GenerateKey("vault_2026")
	if err != nil {
		t.Fatal(err)
	}
	db := pool(t, schema, database.WithEncryption(keyring(t, "vault_2026", first)))
	registry, err := migrate.New(vault.Migrations()...)
	if err != nil {
		t.Fatal(err)
	}
	config := migrate.DefaultPostgresConfig()
	config.Schema, config.SearchPath = schema, schema
	runner, err := migrate.NewPostgres(db, registry, config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Up(t.Context()); err != nil {
		t.Fatal(err)
	}
	settings, err := encrypted.NewJSON(vault.Settings{Region: "sg", Limits: []int{10, 20}})
	if err != nil {
		t.Fatal(err)
	}
	entries := vault.QueryVaultEntries()
	created, err := entries.Create(t.Context(), db, vault.EntryDraft{}.SetLabel("payments").SetToken(encrypted.NewText("sk_live_secret")).SetSettings(settings))
	if err != nil {
		t.Fatal(err)
	}
	if created.Token.Reveal() != "sk_live_secret" {
		t.Fatal("created model does not reveal its plaintext")
	}
	token, rawSettings := stored(t, db, created.ID.String())
	if !strings.HasPrefix(token, "fg1:vault_2026:") || strings.Contains(token, "sk_live_secret") || rawSettings == nil || strings.Contains(*rawSettings, "sg") {
		t.Fatal("stored values are not encryption envelopes", token)
	}
	found, err := entries.RequireFind(t.Context(), db, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := mustSettings(found).Decode()
	if err != nil || found.Token.Reveal() != "sk_live_secret" || decoded.Region != "sg" || len(decoded.Limits) != 2 {
		t.Fatal("hydration did not decrypt", decoded, err)
	}
	// Formatting, JSON and logs never disclose plaintext.
	if text := fmt.Sprintf("%v %+v %#v", found, found, found.Token); strings.Contains(text, "sk_live_secret") {
		t.Fatal("formatting disclosed plaintext")
	}
	if data, _ := json.Marshal(found); strings.Contains(string(data), "sk_live_secret") || strings.Contains(string(data), `"sg"`) {
		t.Fatal("JSON disclosed plaintext")
	}

	// An update that does not assign the field leaves its envelope untouched.
	renamed, err := entries.Update(t.Context(), db, created.ID, vault.EntryDraft{}.SetLabel("billing"))
	if err != nil || renamed.Token.Reveal() != "sk_live_secret" {
		t.Fatal("update without the field changed it", err)
	}
	if after, _ := stored(t, db, created.ID.String()); after != token {
		t.Fatal("unassigned encrypted field was rewritten")
	}
	rotated, err := entries.Update(t.Context(), db, created.ID, vault.EntryDraft{}.SetToken(encrypted.NewText("sk_live_rotated")).ClearSettings())
	if err != nil || rotated.Token.Reveal() != "sk_live_rotated" || !rotated.Settings.IsNull() {
		t.Fatal("encrypted update", err)
	}
	if _, rawSettings := stored(t, db, created.ID.String()); rawSettings != nil {
		t.Fatal("cleared encrypted field is not NULL")
	}

	// Bulk inserts bind each row to its own primary key.
	many, err := entries.CreateMany(t.Context(), db, []vault.EntryDraft{
		vault.EntryDraft{}.SetLabel("a").SetToken(encrypted.NewText("token-a")).ClearSettings(),
		vault.EntryDraft{}.SetLabel("b").SetToken(encrypted.NewText("token-b")).ClearSettings(),
	})
	if err != nil || len(many) != 2 || many[0].Token.Reveal() == many[1].Token.Reveal() {
		t.Fatal("bulk encrypted insert", err)
	}

	// A ciphertext copied into another row does not decrypt there.
	if _, err := db.Exec(t.Context(), "UPDATE vault_entries SET token = (SELECT token FROM vault_entries WHERE id = $2) WHERE id = $1", many[0].ID.String(), many[1].ID.String()); err != nil {
		t.Fatal(err)
	}
	if moved, err := entries.Find(t.Context(), db, many[0].ID); err == nil {
		t.Fatal("copied ciphertext decrypted in another row", moved.IsSet())
	}

	// Without a key ring, encrypted fields neither hydrate nor store plaintext.
	plain := pool(t, schema)
	if _, err := entries.Find(t.Context(), plain, created.ID); !errors.Is(err, fault.Missing) {
		t.Fatal("read without a key ring", err)
	}
	if _, err := entries.Create(t.Context(), plain, vault.EntryDraft{}.SetLabel("x").SetToken(encrypted.NewText("leak")).ClearSettings()); !errors.Is(err, fault.Missing) {
		t.Fatal("write without a key ring", err)
	}

	// Rotation: a new active key writes, the retained key still reads.
	second, err := encryption.GenerateKey("vault_2027")
	if err != nil {
		t.Fatal(err)
	}
	next := pool(t, schema, database.WithEncryption(keyring(t, "vault_2027", second, first)))
	if old, err := entries.RequireFind(t.Context(), next, created.ID); err != nil || old.Token.Reveal() != "sk_live_rotated" {
		t.Fatal("retained key did not decrypt", err)
	}
	fresh, err := entries.Create(t.Context(), next, vault.EntryDraft{}.SetLabel("new").SetToken(encrypted.NewText("token-new")).ClearSettings())
	if err != nil {
		t.Fatal(err)
	}
	if token, _ := stored(t, db, fresh.ID.String()); !strings.HasPrefix(token, "fg1:vault_2027:") {
		t.Fatal("new write did not use the active key", token)
	}

	// Upsert seals the inserted row; a conflict keeps the existing envelope.
	upserted, err := entries.Upsert(t.Context(), next, vault.EntryDraft{}.SetID(fresh.ID).SetLabel("upserted").SetToken(encrypted.NewText("ignored")).ClearSettings(), query.OnConflict(vault.EntryFields().ID).DoUpdate(vault.EntryFields().Label.Incoming()))
	if value, present := upserted.Get(); err != nil || !present || value.Label != "upserted" || value.Token.Reveal() != "token-new" {
		t.Fatal("upsert did not keep the existing encrypted value", err)
	}
	// Set-based writes cannot assign one envelope to many rows.
	if _, err := entries.Where(vault.EntryFields().Label.Eq("a")).Query.PatchAll(t.Context(), next, query.Change(query.AssignEncrypted[vault.Entry]("vault_entries", "token", encrypted.TextCodec(), encrypted.NewText("bulk")))); !errors.Is(err, fault.Invalid) {
		t.Fatal("set-based encrypted write accepted", err)
	}
}

func mustSettings(entry vault.Entry) encrypted.JSON[vault.Settings] {
	settings, _ := entry.Settings.Get()
	return settings
}
