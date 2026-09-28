package mfa

import (
	"encoding/base32"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

func vectorSecret(t *testing.T) TOTPSecret {
	t.Helper()
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))
	key, err := ParseTOTPSecret(secret.New(encoded))
	if err != nil {
		t.Fatal(err)
	}
	return key
}
func instant(t *testing.T, seconds int64) temporal.DateTime {
	t.Helper()
	result, err := temporal.NewDateTime(time.Unix(seconds, 0))
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func testCode(t *testing.T, text string) TOTPCode {
	t.Helper()
	code, err := ParseTOTPCode(secret.New(text))
	if err != nil {
		t.Fatal(err)
	}
	return code
}

func TestTOTPRFC6238SHA1Vectors(t *testing.T) {
	// RFC 6238 appendix B specifies eight digits. The six-digit HOTP variant
	// uses the same dynamic truncation modulo 10^6 (the final six digits).
	key := vectorSecret(t)
	for _, test := range []struct {
		seconds int64
		code    string
	}{
		{59, "287082"}, {1111111109, "081804"}, {1111111111, "050471"},
		{1234567890, "005924"}, {2000000000, "279037"}, {20000000000, "353130"},
	} {
		result, err := matchTOTP(key, testCode(t, test.code), instant(t, test.seconds), 0, value.Optional[int64]{})
		step, ok := result.Get()
		if err != nil || !ok || step != test.seconds/30 {
			t.Fatalf("RFC vector at %d failed: %v", test.seconds, err)
		}
		used, err := matchTOTP(key, testCode(t, test.code), instant(t, test.seconds), 1, result)
		if err != nil || used.IsSet() {
			t.Fatal("accepted step replayed", err)
		}
	}
}

func TestTOTPWindowBoundariesAndMonotonicReplayState(t *testing.T) {
	key := vectorSecret(t)
	raw, _ := totpEncoding.DecodeString(key.Secret().Reveal())
	for offset := int64(-2); offset <= 2; offset++ {
		code := totpAt(raw, uint64(100+offset))
		matched, err := matchTOTP(key, code, instant(t, 3000), 1, value.Optional[int64]{})
		if err != nil {
			t.Fatal(err)
		}
		if matched.IsSet() != (offset >= -1 && offset <= 1) {
			t.Fatal("wrong drift window")
		}
	}
	future := totpAt(raw, 101)
	accepted, err := matchTOTP(key, future, instant(t, 3000), 1, value.Optional[int64]{})
	if err != nil || !accepted.IsSet() {
		t.Fatal("future drift rejected")
	}
	for _, seconds := range []int64{3000, 3029, 3030} {
		replay, err := matchTOTP(key, future, instant(t, seconds), 1, accepted)
		if err != nil || replay.IsSet() {
			t.Fatal("future code replayed")
		}
		older, err := matchTOTP(key, totpAt(raw, 100), instant(t, seconds), 1, accepted)
		if err != nil || older.IsSet() {
			t.Fatal("step moved backwards")
		}
	}
	next, err := matchTOTP(key, totpAt(raw, 102), instant(t, 3060), 0, accepted)
	if err != nil || !next.IsSet() {
		t.Fatal("next step rejected")
	}
	if _, err := matchTOTP(key, future, instant(t, 3000), 2, accepted); err == nil {
		t.Fatal("unbounded window accepted")
	}
	if _, err := matchTOTP(key, future, instant(t, -1), 1, accepted); err == nil {
		t.Fatal("pre-epoch accepted")
	}
	if _, err := matchTOTP(key, future, instant(t, 3000), 1, value.Set(int64(-1))); err == nil {
		t.Fatal("invalid persisted step accepted")
	}
	epoch, err := matchTOTP(key, totpAt(raw, 0), instant(t, 0), 1, value.Optional[int64]{})
	if step, ok := epoch.Get(); err != nil || !ok || step != 0 {
		t.Fatal("epoch overflow", err)
	}
}

func TestTOTPParsingAndProvisioningPreserveExactInputs(t *testing.T) {
	key := vectorSecret(t)
	for _, text := range []string{"", "12345", "1234567", " 123456", "１２３４５６", "12345x", "12 456"} {
		if _, err := ParseTOTPCode(secret.New(text)); err == nil {
			t.Fatal("invalid code accepted")
		}
	}
	code := testCode(t, "001234")
	if code.Secret().Reveal() != "001234" {
		t.Fatal("leading zeros lost")
	}
	if _, err := ParseTOTPSecret(secret.New(strings.ToLower(key.Secret().Reveal()))); err == nil {
		t.Fatal("noncanonical secret accepted")
	}
	generated, err := GenerateTOTPSecret()
	if err != nil || generated.Validate() != nil {
		t.Fatal("generated secret invalid", err)
	}
	uri, err := ProvisioningURI(key, "Foundry 世界", "a/b+user@example.test")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(uri.Reveal())
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != "otpauth" || parsed.Host != "totp" || parsed.Path != "/Foundry 世界:a/b+user@example.test" || parsed.Query().Get("issuer") != "Foundry 世界" || parsed.Query().Get("secret") != key.Secret().Reveal() || parsed.Query().Get("digits") != "6" || parsed.Query().Get("period") != "30" || parsed.Query().Get("algorithm") != "SHA1" {
		t.Fatal("invalid provisioning URI")
	}
	for _, label := range []string{"", "unsafe:label", " leading", "trailing ", "line\nbreak", "\xff", strings.Repeat("x", 257)} {
		if _, err := ProvisioningURI(key, label, "member"); err == nil {
			t.Fatal("invalid issuer accepted")
		}
		if _, err := ProvisioningURI(key, "Foundry", label); err == nil {
			t.Fatal("invalid account accepted")
		}
	}
	for _, item := range []any{key, code, uri} {
		encoded, err := json.Marshal(item)
		if err != nil {
			t.Fatal(err)
		}
		output := fmt.Sprintf("%v %#v %q", item, item, item) + string(encoded)
		for _, raw := range []string{key.Secret().Reveal(), code.Secret().Reveal(), uri.Reveal()} {
			if strings.Contains(output, raw) {
				t.Fatal("factor secret leaked")
			}
		}
	}
	for _, input := range []string{`null`, `123456`, `"bad"`, `{}`, `"1234567"`} {
		decoded := code
		if err := json.Unmarshal([]byte(input), &decoded); err == nil || !decoded.Secret().IsZero() {
			t.Fatal("invalid decoding retained code")
		}
	}
	var decoded TOTPCode
	if err := json.Unmarshal([]byte(`"001234"`), &decoded); err != nil || decoded != code {
		t.Fatal("code JSON input", err)
	}
}
