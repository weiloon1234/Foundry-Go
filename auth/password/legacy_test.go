package password

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

// Imported bcrypt (PHP/Laravel $2y$) and Argon2i hashes verify, and always
// report NeedsRehash so the next successful login stores Argon2id instead.
func TestImportedLegacyHashesVerifyAndRequireRehash(t *testing.T) {
	h := testHasher(t)
	plain := plaintext(t, "imported password")
	generated, err := bcrypt.GenerateFromPassword([]byte("imported password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	php := "$2y$" + string(generated[4:])
	salt := []byte("0123456789abcdef")
	key := argon2.Key([]byte("imported password"), salt, 2, 19*1024, 1, 32)
	argon := fmt.Sprintf("$argon2i$v=%d$m=%d,t=2,p=1$%s$%s", argon2.Version, 19*1024, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key))
	for _, encoded := range []string{string(generated), php, argon} {
		hash, err := ParseHash(secret.New(encoded))
		if err != nil {
			t.Fatal(encoded[:4], err)
		}
		if matched, err := h.Check(t.Context(), plain, hash); err != nil || !matched {
			t.Fatal("legacy hash did not verify", encoded[:4], err)
		}
		if matched, err := h.Check(t.Context(), plaintext(t, "wrong password"), hash); err != nil || matched {
			t.Fatal("legacy hash accepted a wrong password", encoded[:4], err)
		}
		if needs, err := h.NeedsRehash(hash); err != nil || !needs {
			t.Fatal("legacy hash would not be upgraded", encoded[:4], err)
		}
	}
	strict := DefaultConfig()
	strict.Parameters = h.config.Parameters
	strict.MaxBcryptCost = 0
	rejecting, err := New(strict)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := ParseHash(secret.New(php))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rejecting.Check(t.Context(), plain, hash); !errors.Is(err, fault.Invalid) {
		t.Fatal("bcrypt verification was not bounded by policy", err)
	}
	for _, malformed := range []string{"$2x$04$" + strings.Repeat("a", 53), "$2y$99$" + strings.Repeat("a", 53), "$2y$04$" + strings.Repeat("!", 53), php[:59]} {
		if _, err := ParseHash(secret.New(malformed)); !errors.Is(err, fault.Invalid) {
			t.Fatal("malformed bcrypt hash accepted", err)
		}
	}
}
