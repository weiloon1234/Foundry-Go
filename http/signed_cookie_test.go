package http

import (
	"errors"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/testkit"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func cookieTestKeys(t *testing.T, id SigningKeyID, previous ...SigningKey) SigningKeys {
	t.Helper()
	keys, err := NewSigningKeys(SigningKey{ID: id, Secret: secret.New(strings.Repeat(string(id), 32))}, previous...)
	if err != nil {
		t.Fatal(err)
	}
	return keys
}
func cookieTestSigner(t *testing.T, keys SigningKeys, now *testkit.Clock) CookieSigner {
	t.Helper()
	signer, err := NewCookieSigner(keys, now)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}
func signedWire[V any](t *testing.T, c SignedCookie[V], v V) string {
	t.Helper()
	w := httptest.NewRecorder()
	if err := c.Set(t.Context(), w, v); err != nil {
		t.Fatal(err)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("signed cookie absent")
	}
	return cookies[0].Value
}

func TestSignedCookieRotationScopeAndExpiry(t *testing.T) {
	now := testkit.NewClock(time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC))
	options := DefaultCookieOptions()
	options.MaxAge = time.Minute
	base := DefineCookie("__Host-preference", StringCookie[string](), options)
	oldKey := SigningKey{ID: "old", Secret: secret.New(strings.Repeat("s", 32))}
	oldKeys, err := NewSigningKeys(oldKey)
	if err != nil {
		t.Fatal(err)
	}
	old := base.Signed(cookieTestSigner(t, oldKeys, now))
	envelope := signedWire(t, old, "你好; user preference")
	rotated := base.Signed(cookieTestSigner(t, cookieTestKeys(t, "new", oldKey), now))
	got, err := rotated.Read(cookieRequest(base.Name(), envelope))
	v, _ := got.Get()
	if err != nil || v != "你好; user preference" {
		t.Fatalf("rotation failed: %q %v", v, err)
	}
	current := signedWire(t, rotated, "current")
	if !strings.HasPrefix(current, "v1.new.") {
		t.Fatal("new cookie used old key")
	}
	removed := base.Signed(cookieTestSigner(t, cookieTestKeys(t, "new"), now))
	if _, err := removed.Read(cookieRequest(base.Name(), envelope)); !errors.Is(err, ErrInvalidSignedCookie) {
		t.Fatal("retired key accepted")
	}
	other := DefineCookie("__Host-other", StringCookie[string](), options).Signed(rotated.signer)
	if _, err := other.Read(cookieRequest(other.Name(), envelope)); !errors.Is(err, ErrInvalidSignedCookie) {
		t.Fatal("renamed cookie accepted")
	}
	options.HTTPOnly = false
	changedScope := DefineCookie(base.Name(), StringCookie[string](), options).Signed(rotated.signer)
	if _, err := changedScope.Read(cookieRequest(base.Name(), envelope)); !errors.Is(err, ErrInvalidSignedCookie) {
		t.Fatal("changed security scope accepted")
	}
	now.Advance(time.Minute)
	if _, err := rotated.Read(cookieRequest(base.Name(), envelope)); !errors.Is(err, ErrInvalidSignedCookie) || !errors.Is(err, BadRequest) {
		t.Fatalf("expiry boundary accepted: %v", err)
	}
}

func TestSignedCookieVerifiesBeforeDomainDecoderAndRejectsTampering(t *testing.T) {
	now := testkit.NewClock(time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC))
	var decoded atomic.Int32
	codec := cookieCallbacks{format: func(s string) (string, error) { return s, nil }, parse: func(s string) (string, error) { decoded.Add(1); return s, nil }}
	cookie := DefineCookie("selection", codec, DefaultCookieOptions()).Signed(cookieTestSigner(t, cookieTestKeys(t, "key"), now))
	envelope := signedWire(t, cookie, "original")
	parts := strings.Split(envelope, ".")
	for _, tampered := range []string{
		strings.Replace(envelope, "v1.", "v2.", 1),
		strings.Replace(envelope, ".key.", ".unknown.", 1),
		strings.Replace(envelope, ".0.", ".1.", 1),
		strings.Join([]string{parts[0], parts[1], parts[2], "YXR0YWNrZXI", parts[4]}, "."),
		envelope + ".extra", envelope[:len(envelope)-1] + "!",
	} {
		if _, err := cookie.Read(cookieRequest(cookie.Name(), tampered)); !errors.Is(err, ErrInvalidSignedCookie) {
			t.Fatalf("tamper accepted: %v", err)
		}
	}
	if decoded.Load() != 0 {
		t.Fatal("unverified value reached domain codec")
	}
	if value, err := cookie.Read(cookieRequest(cookie.Name(), envelope)); err != nil || !value.IsSet() || decoded.Load() != 1 {
		t.Fatal("valid value was not decoded")
	}
	missing := httptest.NewRequest("GET", "/", nil)
	if value, err := cookie.Read(missing); err != nil || value.IsSet() {
		t.Fatal("missing signed cookie was not optional")
	}
}

func TestSigningKeysValidateAndRedact(t *testing.T) {
	material := strings.Repeat("private-cookie-material", 3)
	key := SigningKey{ID: "key", Secret: secret.New(material)}
	keys, err := NewSigningKeys(key)
	if err != nil {
		t.Fatal(err)
	}
	now := testkit.NewClock(time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC))
	signer := cookieTestSigner(t, keys, now)
	cookie := DefineCookie("value", StringCookie[string](), DefaultCookieOptions()).Signed(signer)
	for _, v := range []any{key, keys, signer, cookie, struct{ Keys SigningKeys }{keys}} {
		for _, format := range []string{"%v", "%+v", "%#v"} {
			if strings.Contains(fmt.Sprintf(format, v), material) {
				t.Fatal("key formatting leaked material")
			}
		}
	}
	for _, key := range []SigningKey{{ID: "", Secret: secret.New(material)}, {ID: "bad.id", Secret: secret.New(material)}, {ID: "key", Secret: secret.New("short")}} {
		if _, err := NewSigningKeys(key); err == nil {
			t.Fatal("bad key accepted")
		}
	}
	if _, err := NewSigningKeys(key, key); err == nil {
		t.Fatal("duplicate key accepted")
	}
	if _, err := NewCookieSigner(SigningKeys{}, now); err == nil {
		t.Fatal("empty keys accepted")
	}
	if _, err := NewCookieSigner(keys, nil); err == nil {
		t.Fatal("nil clock accepted")
	}
}

func TestSignedCookieExplicitEpochExpiryIsNotSessionLifetime(t *testing.T) {
	now := testkit.NewClock(time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC))
	signer := cookieTestSigner(t, cookieTestKeys(t, "key"), now)
	options := DefaultCookieOptions()
	options.Expires = time.Unix(0, 0)
	c := DefineCookie("value", StringCookie[string](), options).Signed(signer)
	w := httptest.NewRecorder()
	if err := c.Set(t.Context(), w, "expired"); err == nil || len(w.Header()) != 0 {
		t.Fatal("epoch expiry became an unlimited session cookie")
	}
}

func TestSignedCookiePersistenceAndRemoval(t *testing.T) {
	now := testkit.NewClock(time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC))
	signer := cookieTestSigner(t, cookieTestKeys(t, "key"), now)
	options := DefaultCookieOptions()
	options.Expires = now.Now().Add(time.Hour)
	c := DefineCookie("value", StringCookie[string](), options).Signed(signer)
	envelope := signedWire(t, c, "")
	value, err := c.Read(cookieRequest(c.Name(), envelope))
	v, ok := value.Get()
	if err != nil || !ok || v != "" {
		t.Fatal("present empty signed cookie lost")
	}
	now.Advance(time.Hour)
	if _, err := c.Read(cookieRequest(c.Name(), envelope)); !errors.Is(err, ErrInvalidSignedCookie) {
		t.Fatal("absolute expiry ignored")
	}
	w := httptest.NewRecorder()
	if err := c.Clear(w); err != nil {
		t.Fatal(err)
	}
	removed := w.Result().Cookies()
	if len(removed) != 1 || removed[0].MaxAge != -1 || removed[0].Name != "value" {
		t.Fatal("signed clear did not use cookie scope")
	}
	options.MaxAge = time.Hour // Native MaxAge takes precedence over the older Expires.
	c = DefineCookie("value", StringCookie[string](), options).Signed(signer)
	if err := c.Set(t.Context(), httptest.NewRecorder(), "live"); err != nil {
		t.Fatalf("MaxAge precedence lost: %v", err)
	}
	options.MaxAge = 0
	c = DefineCookie("value", StringCookie[string](), options).Signed(signer)
	w = httptest.NewRecorder()
	if err := c.Set(t.Context(), w, "expired"); err == nil || len(w.Header()) != 0 {
		t.Fatal("already expired value published")
	}
}
