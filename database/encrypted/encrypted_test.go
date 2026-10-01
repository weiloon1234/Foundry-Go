package encrypted_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/encrypted"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

type preferences struct {
	Theme string `json:"theme"`
}

func keys(t *testing.T) *encryption.Keyring {
	t.Helper()
	key, err := encryption.GenerateKey("field_test")
	if err != nil {
		t.Fatal(err)
	}
	ring, err := encryption.NewKeyring("field_test", key)
	if err != nil {
		t.Fatal(err)
	}
	return ring
}

func binding(t *testing.T, table, column string, key any) encryption.Context {
	t.Helper()
	b, err := encrypted.Binding(table, column, key)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestTextSealsForOneRowAndOpensOnlyThere(t *testing.T) {
	ring := keys(t)
	row := binding(t, "accounts", "token", "0193fd8c-2075-7000-8000-000000000001")
	sealed, err := encrypted.NewText("secret").Seal(t.Context(), ring, row)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := encrypted.TextCodec().Bind(sealed)
	if err != nil || !strings.HasPrefix(stored.(string), "fg1:field_test:") || strings.Contains(stored.(string), "secret") {
		t.Fatal("bound value is not an envelope", stored, err)
	}
	hydrated, err := encrypted.TextCodec().Decode(stored)
	if err != nil || hydrated.Reveal() != "" {
		t.Fatal("decoded value revealed before opening", err)
	}
	opened, err := hydrated.Open(t.Context(), ring, row)
	if err != nil || opened.Reveal() != "secret" {
		t.Fatal("opening the bound row", err)
	}
	for _, other := range []encryption.Context{
		binding(t, "accounts", "token", "0193fd8c-2075-7000-8000-000000000002"),
		binding(t, "accounts", "api_key", "0193fd8c-2075-7000-8000-000000000001"),
		binding(t, "users", "token", "0193fd8c-2075-7000-8000-000000000001"),
		binding(t, "accounts", "token", int64(1)),
	} {
		if _, err := hydrated.Open(t.Context(), ring, other); err == nil {
			t.Fatal("ciphertext opened for another row, column or table")
		}
	}
	// Equal compares plaintext: a fresh envelope of the same value is unchanged.
	again, err := encrypted.NewText("secret").Seal(t.Context(), ring, row)
	if err != nil || !opened.Equal(again) || opened.Equal(encrypted.NewText("other")) {
		t.Fatal("plaintext equality", err)
	}
	if _, err := encrypted.TextCodec().Bind(encrypted.NewText("unsealed")); !errors.Is(err, fault.Invalid) {
		t.Fatal("unsealed plaintext was bound", err)
	}
	if _, err := (encrypted.Text{}).Seal(t.Context(), ring, row); !errors.Is(err, fault.Invalid) {
		t.Fatal("zero value was stored")
	}
	if _, err := encrypted.NewText("x").Seal(t.Context(), nil, row); !errors.Is(err, fault.Missing) {
		t.Fatal("sealed without a key ring")
	}
	for _, key := range []any{"", nil, 1.5, []byte{}} {
		if _, err := encrypted.Binding("accounts", "token", key); !errors.Is(err, fault.Invalid) {
			t.Fatalf("binding accepted key %#v", key)
		}
	}
}

func TestEncryptedValuesAreRedactedEverywhere(t *testing.T) {
	text := encrypted.NewText("secret")
	settings, err := encrypted.NewJSON(preferences{Theme: "dark"})
	if err != nil {
		t.Fatal(err)
	}
	var log strings.Builder
	slog.New(slog.NewTextHandler(&log, nil)).Info("fields", "text", text, "settings", settings)
	data, _ := json.Marshal(map[string]any{"text": text, "settings": settings})
	formatted := fmt.Sprintf("%v %+v %#v %v %#v", text, text, text, settings, settings)
	for _, output := range []string{log.String(), string(data), formatted} {
		if strings.Contains(output, "secret") || strings.Contains(output, "dark") {
			t.Fatal("encrypted value disclosed", output)
		}
	}
	if !encrypted.TextCodec().SensitiveValues() || !encrypted.JSONCodec[preferences]().SensitiveValues() {
		t.Fatal("encrypted codecs are not sensitive")
	}
}

func TestJSONOpensWithTheTypedShapeAndCompareByPlaintext(t *testing.T) {
	ring := keys(t)
	row := binding(t, "accounts", "settings", int64(7))
	settings, err := encrypted.NewJSON(preferences{Theme: "dark"})
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := settings.Seal(t.Context(), ring, row)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := encrypted.JSONCodec[preferences]().Bind(sealed)
	if err != nil {
		t.Fatal(err)
	}
	hydrated, err := encrypted.JSONCodec[preferences]().Decode(stored)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hydrated.Decode(); !errors.Is(err, fault.Invalid) {
		t.Fatal("unopened JSON decoded")
	}
	opened, err := hydrated.Open(t.Context(), ring, row)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := opened.Decode()
	if err != nil || decoded.Theme != "dark" {
		t.Fatal("decrypted JSON", decoded, err)
	}
	// A stored plaintext that no longer fits the type fails instead of hydrating.
	wrong, err := encrypted.NewText(`{"theme":"dark","extra":1}`).Seal(t.Context(), ring, row)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := encrypted.TextCodec().Bind(wrong)
	mismatched, _ := encrypted.JSONCodec[preferences]().Decode(raw)
	if _, err := mismatched.Open(t.Context(), ring, row); err == nil {
		t.Fatal("plaintext with an unknown key opened as the typed shape")
	}
}

func TestChangeDetectionComparesEncryptedPlaintext(t *testing.T) {
	ring := keys(t)
	row := binding(t, "accounts", "token", int64(1))
	before, err := encrypted.NewText("same").Seal(t.Context(), ring, row)
	if err != nil {
		t.Fatal(err)
	}
	after, err := encrypted.NewText("same").Seal(t.Context(), ring, row)
	if err != nil {
		t.Fatal(err)
	}
	unchanged, err := lifecycle.CompareField(encrypted.TextCodec(), value.Set(before), value.Set(after), true)
	if err != nil || unchanged.Changed() {
		t.Fatal("a fresh envelope of the same plaintext reported a change", err)
	}
	other, err := encrypted.NewText("different").Seal(t.Context(), ring, row)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := lifecycle.CompareField(encrypted.TextCodec(), value.Set(before), value.Set(other), true)
	if err != nil || !changed.Changed() {
		t.Fatal("a different plaintext was not a change", err)
	}
	nullable := codec.Nullable(encrypted.TextCodec())
	cleared, err := lifecycle.CompareField(nullable, value.Set(value.Of(before)), value.Set(value.Null[encrypted.Text]()), true)
	if err != nil || !cleared.Changed() || !nullable.ComparesValues() {
		t.Fatal("nullable encrypted comparison", err)
	}
	created, err := lifecycle.CompareField(encrypted.TextCodec(), value.Optional[encrypted.Text]{}, value.Set(encrypted.NewText("unsealed")), true)
	if err != nil || !created.Changed() {
		t.Fatal("creation needs no binding to compare", err)
	}
}
