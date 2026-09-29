package http

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

func encryptedCookieKeyring(t *testing.T, active encryption.KeyID, keys ...encryption.Key) *encryption.Keyring {
	t.Helper()
	keyring, err := encryption.NewKeyring(active, keys...)
	if err != nil {
		t.Fatal(err)
	}
	return keyring
}

func TestEncryptedCookieConfidentialityScopeExpiryAndRotation(t *testing.T) {
	now := testkit.NewClock(time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC))
	old, err := encryption.GenerateKey("cookie-old")
	if err != nil {
		t.Fatal(err)
	}
	current, err := encryption.GenerateKey("cookie-new")
	if err != nil {
		t.Fatal(err)
	}
	encrypter, err := NewCookieEncrypter(encryptedCookieKeyring(t, "cookie-old", old), now)
	if err != nil {
		t.Fatal(err)
	}
	options := DefaultCookieOptions()
	options.MaxAge = time.Hour
	preference := DefineCookie("__Host-preference", StringCookie[string](), options).Encrypted(encrypter)
	w := httptest.NewRecorder()
	if err := preference.Set(t.Context(), w, "private-value; with spaces"); err != nil {
		t.Fatal(err)
	}
	wire := w.Result().Cookies()[0].Value
	if strings.Contains(wire, "private-value") || !strings.HasPrefix(wire, "fg1:cookie-old:") {
		t.Fatalf("cookie value is not an encrypted envelope: %q", wire)
	}
	read := func(c EncryptedCookie[string], text string) (string, error) {
		got, err := c.Read(cookieRequest(c.Name(), text))
		v, _ := got.Get()
		return v, err
	}
	if got, err := read(preference, wire); err != nil || got != "private-value; with spaces" {
		t.Fatalf("round trip=%q %v", got, err)
	}
	// Rotation: a keyring whose active key is new still reads retained-key values.
	rotated, err := NewCookieEncrypter(encryptedCookieKeyring(t, "cookie-new", current, old), now)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := read(DefineCookie("__Host-preference", StringCookie[string](), options).Encrypted(rotated), wire); err != nil || got != "private-value; with spaces" {
		t.Fatalf("retained key failed: %q %v", got, err)
	}
	retired, _ := NewCookieEncrypter(encryptedCookieKeyring(t, "cookie-new", current), now)
	scoped := options
	scoped.Path = "/"
	scoped.HTTPOnly = false
	for _, reader := range []EncryptedCookie[string]{
		DefineCookie("__Host-preference", StringCookie[string](), options).Encrypted(retired),
		DefineCookie("__Host-preference", StringCookie[string](), scoped).Encrypted(encrypter),
	} {
		if _, err := read(reader, wire); !errors.Is(err, BadRequest) || !errors.Is(err, ErrInvalidEncryptedCookie) {
			t.Fatalf("retired key or changed scope accepted: %v", err)
		}
	}
	renamed := DefineCookie("__Host-other", StringCookie[string](), options).Encrypted(encrypter)
	if _, err := read(renamed, wire); !errors.Is(err, ErrInvalidEncryptedCookie) {
		t.Fatal("ciphertext moved to another cookie name")
	}
	for _, tampered := range []string{wire[:len(wire)-2] + "AA", "fg1:cookie-old:", "plain-text", strings.Replace(wire, "cookie-old", "cookie-new", 1)} {
		if _, err := read(preference, tampered); !errors.Is(err, BadRequest) {
			t.Fatalf("tampered value accepted: %q %v", tampered, err)
		}
	}
	now.Advance(time.Hour)
	if _, err := read(preference, wire); !errors.Is(err, ErrInvalidEncryptedCookie) {
		t.Fatal("expired encrypted cookie accepted")
	}
	if absent, err := preference.Read(httptest.NewRequest("GET", "/", nil)); err != nil || absent.IsSet() {
		t.Fatal("absent encrypted cookie became present", err)
	}
	cleared := httptest.NewRecorder()
	if err := preference.Clear(cleared); err != nil || cleared.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("encrypted cookie removal failed", err)
	}
	if _, err := NewCookieEncrypter(nil, now); err == nil {
		t.Fatal("missing keyring accepted")
	}
	if _, err := NewCookieEncrypter(encryptedCookieKeyring(t, "cookie-old", old), nil); err == nil {
		t.Fatal("missing clock accepted")
	}
	if got := strings.Join([]string{encrypter.String(), encrypter.GoString()}, ","); strings.Contains(got, "cookie") {
		t.Fatal("encrypter formatting exposed configuration")
	}
	oversized := httptest.NewRecorder()
	if err := preference.Set(t.Context(), oversized, strings.Repeat("x", 3500)); err == nil || len(oversized.Header().Values("Set-Cookie")) != 0 {
		t.Fatal("oversized encrypted cookie published")
	}
}
