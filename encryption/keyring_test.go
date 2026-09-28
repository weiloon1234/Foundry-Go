package encryption

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
)

func testKey(t *testing.T, id KeyID) Key {
	t.Helper()
	key, err := GenerateKey(id)
	if err != nil {
		t.Fatal(err)
	}
	return key
}
func testRing(t *testing.T, active KeyID, keys ...Key) *Keyring {
	t.Helper()
	ring, err := NewKeyring(active, keys...)
	if err != nil {
		t.Fatal(err)
	}
	return ring
}
func testContext(t *testing.T, purpose Purpose, binding string) Context {
	t.Helper()
	result, err := NewContext(purpose, secret.New(binding))
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestEnvelopeAuthenticatesKeyIDPurposeAndExactOwner(t *testing.T) {
	key := testKey(t, "primary")
	ring := testRing(t, key.ID(), key)
	binding := testContext(t, "auth.mfa.totp.v1", "tenant:one/member:7/factor:1")
	plain := secret.New("sensitive-factor-value")
	encrypted, err := ring.Encrypt(t.Context(), binding, plain)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseCiphertext(encrypted.Encoded())
	if err != nil {
		t.Fatal(err)
	}
	got, err := ring.Decrypt(t.Context(), binding, parsed)
	if err != nil || got != plain {
		t.Fatal("round trip", err)
	}
	other, err := ring.Encrypt(t.Context(), binding, plain)
	if err != nil || other == encrypted {
		t.Fatal("nonce reused", err)
	}
	for _, wrong := range []Context{
		testContext(t, "auth.password.v1", "tenant:one/member:7/factor:1"),
		testContext(t, "auth.mfa.totp.v1", "tenant:two/member:7/factor:1"),
		testContext(t, "auth.mfa.totp.v1", "tenant:one/member:8/factor:1"),
		testContext(t, "auth.mfa.totp.v1", "tenant:one/member:7/factor:2"),
	} {
		got, err := ring.Decrypt(t.Context(), wrong, encrypted)
		if err == nil || !got.IsZero() {
			t.Fatal("context substitution accepted")
		}
	}
	// Even the same material under a renamed key ID cannot authenticate the
	// envelope header. Separate rings model an erroneous operator remapping.
	renamed, err := ParseKey("renamed", key.Secret())
	if err != nil {
		t.Fatal(err)
	}
	changed, err := ParseCiphertext(strings.Replace(encrypted.Encoded(), ":primary:", ":renamed:", 1))
	if err != nil {
		t.Fatal(err)
	}
	got, err = testRing(t, renamed.ID(), renamed).Decrypt(t.Context(), binding, changed)
	if err == nil || !got.IsZero() {
		t.Fatal("key ID not authenticated")
	}
	// Binary contexts must not collapse through JSON's UTF-8 replacement.
	binary := testContext(t, "record", "\xff")
	value, err := ring.Encrypt(t.Context(), binary, plain)
	if err != nil {
		t.Fatal(err)
	}
	got, err = ring.Decrypt(t.Context(), testContext(t, "record", "\ufffd"), value)
	if err == nil || !got.IsZero() {
		t.Fatal("binary context normalized")
	}
}

func TestEnvelopeRejectsEveryChangedByte(t *testing.T) {
	key := testKey(t, "primary")
	ring := testRing(t, key.ID(), key)
	binding := testContext(t, "test", "record-1")
	ciphertext, err := ring.Encrypt(t.Context(), binding, secret.New("private"))
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(ciphertext.Encoded(), ":")
	raw, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	for i := range raw {
		changed := bytes.Clone(raw)
		changed[i] ^= 1
		parsed, err := ParseCiphertext(parts[0] + ":" + parts[1] + ":" + base64.RawURLEncoding.EncodeToString(changed))
		if err != nil {
			t.Fatal(err)
		}
		got, err := ring.Decrypt(t.Context(), binding, parsed)
		if err == nil || !got.IsZero() {
			t.Fatalf("modified envelope byte %d accepted", i)
		}
	}
}

func TestKeyRotationRequiresRetainedKeyAndPreservesBinding(t *testing.T) {
	old, next := testKey(t, "old"), testKey(t, "next")
	before := testRing(t, old.ID(), old)
	during := testRing(t, next.ID(), old, next)
	after := testRing(t, next.ID(), next)
	binding := testContext(t, "test", "member-7/factor-9")
	original, err := before.Encrypt(t.Context(), binding, secret.New("unchanged-secret"))
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := during.Reencrypt(t.Context(), binding, original)
	if err != nil || rotated.KeyID() != next.ID() {
		t.Fatal("rotation", err)
	}
	got, err := after.Decrypt(t.Context(), binding, rotated)
	if err != nil || got.Reveal() != "unchanged-secret" {
		t.Fatal("rotated decryption", err)
	}
	got, err = after.Decrypt(t.Context(), binding, original)
	if err == nil || !got.IsZero() {
		t.Fatal("removed key fell back to active")
	}
	wrong := testKey(t, old.ID())
	got, err = testRing(t, wrong.ID(), wrong).Decrypt(t.Context(), binding, original)
	if err == nil || !got.IsZero() {
		t.Fatal("wrong key accepted")
	}
}

func TestEncryptionBoundsAndCancellationReturnNoData(t *testing.T) {
	key := testKey(t, "key")
	ring := testRing(t, key.ID(), key)
	binding := testContext(t, "test", "record")
	for _, size := range []int{0, MaxPlaintextBytes} {
		plain := secret.New(strings.Repeat("x", size))
		encrypted, err := ring.Encrypt(t.Context(), binding, plain)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := ParseCiphertext(encrypted.Encoded())
		if err != nil {
			t.Fatal(err)
		}
		got, err := ring.Decrypt(t.Context(), binding, parsed)
		if err != nil || got != plain {
			t.Fatal("boundary round trip", err)
		}
	}
	encrypted, err := ring.Encrypt(t.Context(), binding, secret.New(strings.Repeat("x", MaxPlaintextBytes+1)))
	if !errors.Is(err, fault.Invalid) || !encrypted.IsZero() {
		t.Fatal("oversize plaintext accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	encrypted, err = ring.Encrypt(ctx, binding, secret.New("private"))
	if !errors.Is(err, context.Canceled) || !encrypted.IsZero() {
		t.Fatal("canceled encryption published")
	}
	for _, invalid := range []Context{{}, {purpose: "test"}, {binding: "record"}} {
		encrypted, err := ring.Encrypt(t.Context(), invalid, secret.New("private"))
		if err == nil || !encrypted.IsZero() {
			t.Fatal("invalid context accepted")
		}
	}
	for _, bad := range []string{"", "fg0:key:AAAA", "fg1:BAD:AAAA", "fg1:key:AAAA", "fg1:key:" + strings.Repeat("A", maxEnvelopeBytes)} {
		if _, err := ParseCiphertext(bad); err == nil {
			t.Fatal("invalid envelope accepted")
		}
	}
	if _, err := NewContext("test", secret.New(strings.Repeat("x", MaxBindingBytes+1))); err == nil {
		t.Fatal("oversize binding accepted")
	}
	if _, err := (*Keyring)(nil).Encrypt(t.Context(), binding, secret.New("private")); err == nil {
		t.Fatal("nil ring accepted")
	}
	if _, err := ring.Encrypt(nil, binding, secret.New("private")); err == nil {
		t.Fatal("nil context accepted")
	}
}

func TestKeyringRejectsDuplicateMaterialAndSharesLocalBudget(t *testing.T) {
	key := testKey(t, "key")
	alias, err := ParseKey("alias", key.Secret())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewKeyring(key.ID(), key, alias); !errors.Is(err, fault.Duplicate) {
		t.Fatal("duplicate material accepted", err)
	}
	if _, err := NewKeyring(key.ID(), key, key); !errors.Is(err, fault.Duplicate) {
		t.Fatal("duplicate ID accepted", err)
	}
	if _, err := NewKeyring("missing", key); err == nil {
		t.Fatal("missing active key accepted")
	}
	if _, err := NewKeyring(key.ID(), Key{}); err == nil {
		t.Fatal("zero key accepted")
	}
	key.state.encryptions.Store(maxEncryptions - 1)
	rings := []*Keyring{testRing(t, key.ID(), key), testRing(t, key.ID(), key)}
	binding := testContext(t, "test", "record")
	results := make(chan error, 16)
	var wait sync.WaitGroup
	for i := range 16 {
		wait.Go(func() { _, err := rings[i%2].Encrypt(t.Context(), binding, secret.New("private")); results <- err })
	}
	wait.Wait()
	close(results)
	successful := 0
	for err := range results {
		if err == nil {
			successful++
		} else if !errors.Is(err, fault.Internal) {
			t.Fatal(err)
		}
	}
	if successful != 1 || key.state.encryptions.Load() != maxEncryptions {
		t.Fatal("local budget exceeded")
	}
}

func TestEncryptionValuesRedactFormattingJSONAndSlog(t *testing.T) {
	key := testKey(t, "key")
	ring := testRing(t, key.ID(), key)
	binding := testContext(t, "test", "sensitive-binding")
	ciphertext, err := ring.Encrypt(t.Context(), binding, secret.New("sensitive-plaintext"))
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []any{key, &key, ring, binding, ciphertext} {
		var logs bytes.Buffer
		slog.New(slog.NewTextHandler(&logs, nil)).Info("test", "value", item)
		encoded, err := json.Marshal(item)
		if err != nil {
			t.Fatal(err)
		}
		output := fmt.Sprintf("%v %+v %#v %s %q", item, item, item, item, item) + string(encoded) + logs.String()
		for _, raw := range []string{key.Secret().Reveal(), ciphertext.Encoded(), "sensitive-binding", "sensitive-plaintext"} {
			if strings.Contains(output, raw) {
				t.Fatal("sensitive encryption value leaked")
			}
		}
	}
}

func FuzzCiphertextParsing(f *testing.F) {
	f.Add("fg1:key:AAAA")
	f.Add("")
	f.Fuzz(func(t *testing.T, encoded string) {
		parsed, err := ParseCiphertext(encoded)
		if err == nil && parsed.Encoded() != encoded {
			t.Fatal("parser normalized ciphertext")
		}
	})
}
