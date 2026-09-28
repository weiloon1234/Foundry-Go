// Package idempotency records one committed local outcome per retained scoped key.
// Callbacks use the supplied transaction for every business write and outbox
// enqueue. Network delivery and independently captured resources are not atomic.
package idempotency

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"regexp"
	"unicode"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

// Namespace separates application deployments sharing the same store.
type Namespace = keyspace.Namespace

// OperationID names a stable business operation, independently of its URL.
type OperationID string

// Definition versions both the operation's meaning and its wire fingerprint.
// A version change creates a new address and can execute an old client key again.
type Definition struct {
	ID      OperationID
	Version uint32
}

func (d Definition) Validate() error {
	if !identifier.Semantic(string(d.ID)) || d.Version == 0 {
		return invalid("operation needs a semantic identity and nonzero version")
	}
	return nil
}

const MinKeyBytes = 16
const MaxKeyBytes = 256

// Key is an opaque client submission identity. Parsing does not establish entropy.
// Use a random UUID or stronger random value, and reuse it for network retries.
type Key struct{ text string }

func ParseKey(text string) (Key, error) {
	if len(text) < MinKeyBytes || len(text) > MaxKeyBytes {
		return Key{}, BadKey
	}
	if !keyPattern.MatchString(text) {
		return Key{}, BadKey
	}
	return Key{text}, nil
}
func (Key) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("idempotency key")) }

// Scope contains trusted tenant and caller identity. HTTP adapters derive this
// from verified authentication on every request; it is never read from the key.
// Webhook adapters verify the provider signature before constructing their scope.
type Scope struct{ digest string }

func NewScope(tenant, caller string) (Scope, error) {
	if !identityPart(tenant) || !identityPart(caller) {
		return Scope{}, invalid("idempotency scope needs bounded trusted tenant and caller identities")
	}
	return Scope{digest: digest("foundry.idempotency.scope.v1", tenant, caller)}, nil
}
func identityPart(s string) bool {
	if len(s) == 0 || len(s) > 256 || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return false
		}
	}
	return true
}
func (Scope) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("idempotency scope")) }
func digest(domain string, values ...string) string {
	h := sha256.New()
	for _, v := range append([]string{domain}, values...) {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(v)))
		_, _ = h.Write(size[:])
		_, _ = h.Write([]byte(v))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// KeyPattern is shared with HTTP metadata and generated clients.
func KeyPattern(maximum int) string {
	return fmt.Sprintf("^[A-Za-z0-9._:-]{%d,%d}$", MinKeyBytes, maximum)
}

var keyPattern = regexp.MustCompile(KeyPattern(MaxKeyBytes))
