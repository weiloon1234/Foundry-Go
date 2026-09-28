package storage_test

import (
	"encoding/json"
	"github.com/weiloon1234/Foundry-Go/storage"
	"strings"
	"testing"
)

func TestObjectKeysRejectAmbiguityWithoutNormalization(t *testing.T) {
	for _, text := range []string{"", "/absolute", "trailing/", "a//b", "a/../b", "./x", "a\\b", "bad\x00key", strings.Repeat("x", storage.MaxKeyBytes+1), string([]byte{255})} {
		if _, err := storage.ParseKey(text); err == nil {
			t.Fatal("invalid key accepted")
		}
	}
	for _, text := range []string{"a b", "literal/%2e%2e", "case/AbC", "emoji/😀", "dot/.hidden", "colon:a", "query/?#"} {
		key, err := storage.ParseKey(text)
		if err != nil || key.String() != text {
			t.Fatal("key normalized", err)
		}
		encoded, err := json.Marshal(key)
		if err != nil {
			t.Fatal(err)
		}
		var decoded storage.ObjectKey
		if err := json.Unmarshal(encoded, &decoded); err != nil || decoded != key {
			t.Fatal("JSON key identity changed", err)
		}
	}
	key, _ := storage.ParseKey("existing")
	if err := json.Unmarshal([]byte(`null`), &key); err == nil || !key.IsZero() {
		t.Fatal("failed parse retained prior key")
	}
}
func FuzzObjectKeyJSON(f *testing.F) {
	for _, seed := range []string{`"files/report.csv"`, `null`, `"%2e%2e"`, `"a/../b"`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		var key storage.ObjectKey
		if err := json.Unmarshal([]byte(text), &key); err == nil {
			if err := key.Validate(); err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(key)
			if err != nil {
				t.Fatal(err)
			}
			var again storage.ObjectKey
			if err := json.Unmarshal(raw, &again); err != nil || again != key {
				t.Fatal("key did not round trip", err)
			}
		}
	})
}
