package challenge_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth/challenge"
	"github.com/weiloon1234/Foundry-Go/secret"
)

func TestBindingPreservesExactComponentBoundaries(t *testing.T) {
	one, err := challenge.Bind(secret.New("a|b"), secret.New("c"))
	if err != nil {
		t.Fatal(err)
	}
	same, err := challenge.Bind(secret.New("a|b"), secret.New("c"))
	if err != nil {
		t.Fatal(err)
	}
	other, err := challenge.Bind(secret.New("a"), secret.New("b|c"))
	if err != nil {
		t.Fatal(err)
	}
	if !one.Equal(same) || one.Equal(other) {
		t.Fatal("binding component collision")
	}
	restored, err := challenge.ParseBinding(one.Hex())
	if err != nil || !restored.Equal(one) {
		t.Fatal("binding round trip", err)
	}
	for _, parts := range [][]secret.String{nil, {secret.New("")}, {secret.New(strings.Repeat("x", 4097))}, {secret.New(string([]byte{0xff}))}} {
		if _, err := challenge.Bind(parts...); err == nil {
			t.Fatal("invalid binding accepted")
		}
	}
	if strings.Contains(fmt.Sprintf("%+v", one), one.Hex()) {
		t.Fatal("binding formatting disclosed digest")
	}
}
func TestTokenSyntaxPurposeAndRedaction(t *testing.T) {
	// 32 zero bytes have a canonical base64url representation; entropy is an
	// issuance concern, while parsing checks syntax without asserting authority.
	raw := strings.Repeat("A", 43)
	token, err := challenge.ParseToken[struct{}, challenge.PasswordReset](secret.New(raw))
	if err != nil {
		t.Fatal(err)
	}
	if err := token.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", raw + "=", raw[:42] + "B", strings.Repeat("a", 1000)} {
		if _, err := challenge.ParseToken[struct{}, challenge.PasswordReset](secret.New(bad)); err == nil {
			t.Fatal("invalid token accepted")
		}
	}
	data, err := json.Marshal(token)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), raw) || strings.Contains(fmt.Sprintf("%#v", token), raw) {
		t.Fatal("token disclosed")
	}
	for _, bad := range []time.Duration{0, time.Nanosecond, challenge.MaxLifetime + time.Microsecond} {
		if err := challenge.ValidateLifetime(bad); err == nil {
			t.Fatal("invalid lifetime accepted")
		}
	}
}
