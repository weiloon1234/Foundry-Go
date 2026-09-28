package randomtoken

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestRandomTokenEncodingsBoundsAndRedaction(t *testing.T) {
	if rejectionCutoff != 248 || rejectionCutoff%len(alphabet) != 0 {
		t.Fatal("biased character selection")
	}
	for _, size := range []int{1, 32, MaxBytes} {
		raw, err := Bytes(size)
		if err != nil || len(raw) != size {
			t.Fatal(err)
		}
		token, err := Base64(size)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := base64.RawURLEncoding.Strict().DecodeString(token.Reveal())
		if err != nil || len(decoded) != size {
			t.Fatal(err)
		}
		hexToken, err := Hex(size)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err = hex.DecodeString(hexToken.Reveal())
		if err != nil || len(decoded) != size {
			t.Fatal(err)
		}
		characters, err := Generate(size)
		if err != nil || len(characters.Reveal()) != size {
			t.Fatal(err)
		}
		for _, c := range characters.Reveal() {
			if !strings.ContainsRune(alphabet, c) {
				t.Fatal("unexpected token alphabet")
			}
		}
	}
	for _, size := range []int{-1, 0, MaxBytes + 1} {
		if _, err := Bytes(size); err == nil {
			t.Fatal("unbounded entropy allocation")
		}
		if _, err := Generate(size); err == nil {
			t.Fatal("unbounded character allocation")
		}
	}
	token, err := Base64(32)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(token)
	if strings.Contains(fmt.Sprintf("%v %#v %s", token, token, encoded), token.Reveal()) {
		t.Fatal("opaque token leaked through formatting")
	}
}
