package challenge

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/secret"
)

type tokenJSONMember struct{}
type tokenJSONAdmin struct{}

func TestChallengeJSONKeepsModelPurposeAndRedaction(t *testing.T) {
	const raw = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	token, err := ParseToken[tokenJSONMember, PasswordReset](secret.New(raw))
	if err != nil {
		t.Fatal(err)
	}
	reset, err := token.JSONContract().Description()
	if err != nil {
		t.Fatal(err)
	}
	verify, err := (Token[tokenJSONMember, EmailVerification]{}).JSONContract().Description()
	if err != nil {
		t.Fatal(err)
	}
	other, err := (Token[tokenJSONAdmin, PasswordReset]{}).JSONContract().Description()
	if err != nil {
		t.Fatal(err)
	}
	if reset.Root == verify.Root || reset.Root == other.Root || reset.Types[0].Kind != contract.StringKind {
		t.Fatal("challenge owner/purpose contract erased")
	}
	limits := contract.JSONLimits{Bytes: 1024, Depth: 4, Nodes: 20, Steps: 40, Issues: 4}
	parsed, err := token.JSONContract().Decode(t.Context(), []byte(`"`+raw+`"`), limits)
	if err != nil || parsed.Secret() != token.Secret() {
		t.Fatal("typed challenge decode", err)
	}
	encoded, err := token.JSONContract().Encode(t.Context(), parsed, limits)
	if err != nil || string(encoded) != `"[REDACTED]"` || strings.Contains(fmt.Sprintf("%#v", parsed), raw) {
		t.Fatal("challenge disclosure", err)
	}
	for _, bad := range []string{`null`, `42`, `{}`, `""`, `"bad"`, `"` + raw + `="`, `"` + raw + ` "`} {
		current := token
		if err := json.Unmarshal([]byte(bad), &current); err == nil || !current.Secret().IsZero() {
			t.Fatal("invalid JSON retained token", err)
		}
	}
	var absent *Token[tokenJSONMember, PasswordReset]
	if absent.UnmarshalJSON([]byte(`"`+raw+`"`)) == nil {
		t.Fatal("nil token destination accepted")
	}
}
func FuzzChallengeJSON(f *testing.F) {
	f.Add([]byte(`"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"`))
	f.Add([]byte(`null`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var token Token[tokenJSONMember, PasswordReset]
		err := token.UnmarshalJSON(data)
		if err != nil {
			if !token.Secret().IsZero() {
				t.Fatal("failed parse retained token")
			}
			return
		}
		if err := token.Validate(); err != nil {
			t.Fatal("successful parse is invalid", err)
		}
		encoded, err := json.Marshal(token)
		if err != nil || string(encoded) != `"[REDACTED]"` {
			t.Fatal("parsed token did not redact", err)
		}
	})
}
