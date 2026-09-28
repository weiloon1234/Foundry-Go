package jobs

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// UniqueKey is a payload-owned business identity, separate from a dispatch ID.
// Its hash is captured in the envelope; raw key text is never persisted.
type UniqueKey[P any] struct{ text string }

func NewUniqueKey[P any](text string) (UniqueKey[P], error) {
	if len(text) == 0 || len(text) > 4096 {
		return UniqueKey[P]{}, fault.New(fault.Invalid, "job uniqueness key must contain 1 to 4096 bytes")
	}
	return UniqueKey[P]{text: text}, nil
}
func (UniqueKey[P]) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("job uniqueness key")) }

// Unique suppresses other dispatch identities for the same name, version, queue
// and key for a fixed window from successful enqueue. Completion does not release
// that window. It is admission deduplication, not exactly-once execution.
type Unique[P any] struct {
	Key UniqueKey[P]
	For time.Duration
}

// Uniqueness is the immutable heterogeneous adapter contract.
type Uniqueness struct {
	Digest string        `json:"digest"`
	For    time.Duration `json:"for"`
}

func (u Uniqueness) Validate() error {
	if u.Digest == "" && u.For == 0 {
		return nil
	}
	data, err := hex.DecodeString(u.Digest)
	if err != nil || len(data) != sha256.Size || hex.EncodeToString(data) != u.Digest || u.For < time.Millisecond || u.For > MaxDelay || u.For%time.Millisecond != 0 {
		return fault.New(fault.Invalid, "invalid job uniqueness window")
	}
	return nil
}
func (u Unique[P]) capture(name Name, version Version) (Uniqueness, error) {
	if u.Key.text == "" && u.For == 0 {
		return Uniqueness{}, nil
	}
	if u.Key.text == "" {
		return Uniqueness{}, fault.New(fault.Invalid, "job uniqueness requires a key")
	}
	digest := sha256.Sum256([]byte(string(name) + "\x00" + strconv.FormatUint(uint64(version), 10) + "\x00" + u.Key.text))
	result := Uniqueness{Digest: hex.EncodeToString(digest[:]), For: u.For}
	return result, result.Validate()
}

var ErrNotUnique = fault.New(fault.Conflict, "job uniqueness window is active")
