package credential

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"testing"

	"github.com/weiloon1234/Foundry-Go/secret"
)

func TestRandomTokenSharedGenerationPreservesCredentialWireFormat(t *testing.T) {
	for range 20 {
		token, digest, err := New()
		if err != nil {
			t.Fatal(err)
		}
		raw, err := base64.RawURLEncoding.Strict().DecodeString(token.Reveal())
		if err != nil || len(raw) != SecretBytes || len(token.Reveal()) != EncodedSecretBytes {
			t.Fatal("credential format changed", err)
		}
		expected := sha256.Sum256(raw)
		if digest.Hex() != hex.EncodeToString(expected[:]) {
			t.Fatal("credential digest changed")
		}
		again, err := Hash(token)
		if err != nil || !again.Equal(digest) {
			t.Fatal(err)
		}
	}
	raw := make([]byte, SecretBytes)
	for i := range raw {
		raw[i] = byte(i)
	}
	digest, err := Hash(secret.New(base64.RawURLEncoding.EncodeToString(raw)))
	if err != nil {
		t.Fatal(err)
	}
	expected := sha256.Sum256(raw)
	if digest.Hex() != hex.EncodeToString(expected[:]) {
		t.Fatal("existing persisted token compatibility changed")
	}
}
