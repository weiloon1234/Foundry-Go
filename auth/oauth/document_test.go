package oauth

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestProviderDocumentRejectsTrailingData(t *testing.T) {
	for _, suffix := range []string{"}", "]", "{}", "null", "true", "garbage"} {
		var target map[string]json.RawMessage
		if err := decodeDocument([]byte(`{"sub":"member"}`+suffix), &target); err == nil {
			t.Fatal("provider document accepted trailing data", suffix)
		}
	}
	var target map[string]json.RawMessage
	if err := decodeDocument([]byte("{\"sub\":\"member\"}\n\t "), &target); err != nil {
		t.Fatal("valid provider document rejected", err)
	}
}

func TestRSAKeyMinimumUsesBits(t *testing.T) {
	for _, first := range []byte{1, 0x7f, 0x80, 0xff} {
		modulus := make([]byte, 256)
		modulus[0], modulus[255] = first, 1
		_, accepted := parseKey(jsonWebKey{KeyType: "RSA", N: base64.RawURLEncoding.EncodeToString(modulus), E: "AQAB"})
		if accepted != (first >= 0x80) {
			t.Fatal("incorrect RSA modulus strength", first)
		}
	}
}
