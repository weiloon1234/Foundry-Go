package mfa

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/secret"
)

func TestRecoveryCodesHaveIndependentOwnedSingleUseState(t *testing.T) {
	codes, hashes, err := newRecoveryCodes(DefaultRecoveryCodes)
	if err != nil {
		t.Fatal(err)
	}
	original := slices.Clone(hashes)
	for i, code := range codes {
		if len(code.Secret().Reveal()) != 43 || code.Validate() != nil {
			t.Fatal("invalid generated recovery code")
		}
		parsed, err := ParseRecoveryHash(hashes[i].Encoded())
		if err != nil || parsed != hashes[i] {
			t.Fatal("hash persistence", err)
		}
		after, matched, err := consumeRecovery(original, code)
		if err != nil || !matched || len(after) != len(original)-1 || !slices.Equal(hashes, original) {
			t.Fatal("consumption changed snapshot", err)
		}
		if _, reused, err := consumeRecovery(after, code); err != nil || reused {
			t.Fatal("consumed code replayed", err)
		}
		if len(after) > 0 {
			after[0] = RecoveryHash{}
		}
		if !slices.Equal(hashes, original) {
			t.Fatal("returned hashes alias original")
		}
		output, err := json.Marshal(code)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(output)+fmt.Sprintf("%#v", code), code.Secret().Reveal()) {
			t.Fatal("recovery code leaked")
		}
		var decoded RecoveryCode
		input, _ := json.Marshal(code.Secret().Reveal())
		if err := json.Unmarshal(input, &decoded); err != nil || decoded != code {
			t.Fatal("recovery input", err)
		}
		if err := json.Unmarshal([]byte(`null`), &decoded); err == nil || !decoded.Secret().IsZero() {
			t.Fatal("invalid decode retained code")
		}
	}
	state := hashes
	for _, code := range codes {
		var matched bool
		state, matched, err = consumeRecovery(state, code)
		if err != nil || !matched {
			t.Fatal("independent recovery code failed", err)
		}
	}
	if len(state) != 0 {
		t.Fatal("not all codes consumed")
	}
	if _, matched, err := consumeRecovery(state, codes[0]); err != nil || matched {
		t.Fatal("empty state accepted replay")
	}
	newer, _, err := newRecoveryCodes(1)
	if err != nil {
		t.Fatal(err)
	}
	if _, matched, err := consumeRecovery(hashes, newer[0]); err != nil || matched {
		t.Fatal("unrelated code accepted")
	}
}

func TestRecoveryStateAndInputAreBounded(t *testing.T) {
	codes, hashes, err := newRecoveryCodes(1)
	if err != nil {
		t.Fatal(err)
	}
	for _, count := range []int{0, -1, MaxRecoveryCodes + 1} {
		if a, b, err := newRecoveryCodes(count); err == nil || a != nil || b != nil {
			t.Fatal("invalid count accepted")
		}
	}
	for _, bad := range [][]RecoveryHash{{{}}, {hashes[0], hashes[0]}, make([]RecoveryHash, MaxRecoveryCodes+1)} {
		if _, matched, err := consumeRecovery(bad, codes[0]); err == nil || matched {
			t.Fatal("invalid recovery state accepted")
		}
	}
	for _, raw := range []string{"", "123456", codes[0].Secret().Reveal() + "=", strings.Repeat("/", 43), strings.Repeat("A", 42) + "B"} {
		if _, err := ParseRecoveryCode(secret.New(raw)); err == nil {
			t.Fatal("invalid recovery input accepted")
		}
	}
}
