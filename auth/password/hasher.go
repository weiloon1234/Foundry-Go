package password

import (
	"context"
	"crypto/rand"
	"crypto/subtle"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
	"golang.org/x/crypto/argon2"
)

// Hasher owns bounded CPU/memory admission. Share one instance across the
// application's login/reset paths. It owns no database, models or goroutines
// between operations; constructing it performs no hashing or other I/O.
type Hasher struct {
	config Config
	gate   *credential.Gate
}

func New(config Config) (*Hasher, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Hasher{config: config, gate: credential.NewGate(config.MaxConcurrent, config.Timeout)}, nil
}
func (h *Hasher) Validate() error {
	if h == nil || h.gate == nil {
		return fault.New(fault.Invalid, "password hasher is not configured")
	}
	return h.config.Validate()
}

// Hash uses a fresh random salt and the configured issuance policy. Error or
// cancellation returns zero Hash, including when the KDF finishes after timeout.
func (h *Hasher) Hash(ctx context.Context, plain Plaintext) (Hash, error) {
	if err := h.Validate(); err != nil {
		return Hash{}, err
	}
	if err := plain.Validate(); err != nil {
		return Hash{}, err
	}
	var result Hash
	err := h.gate.Execute(ctx, func(op context.Context) error {
		salt := make([]byte, SaltBytes)
		if _, err := rand.Read(salt); err != nil {
			return fault.Wrap(fault.Internal, "password salt generation failed", err)
		}
		if err := op.Err(); err != nil {
			return err
		}
		p := h.config.Parameters
		input := []byte(plain.value.Reveal())
		defer clear(input)
		key := argon2.IDKey(input, salt, p.Iterations, p.MemoryKiB, p.Parallelism, KeyBytes)
		defer clear(key)
		result = encodedHash(p, salt, key)
		return nil
	})
	if err != nil {
		return Hash{}, err
	}
	return result, nil
}

// Check compares derived keys in constant time. A mismatch is false with no
// error; malformed/unsupported hashes and resource-ceiling violations are errors.
// Login adapters must expose one unauthenticated response without disclosing
// which condition occurred. Bounds are checked before allocating Argon2 memory.
func (h *Hasher) Check(ctx context.Context, plain Plaintext, hash Hash) (bool, error) {
	if err := h.Validate(); err != nil {
		return false, err
	}
	if err := plain.Validate(); err != nil {
		return false, err
	}
	parsed, err := parse(hash.encoded.Reveal())
	if err != nil {
		return false, err
	}
	if !parsed.parameters.within(h.config.VerifyLimit) {
		return false, fault.New(fault.Invalid, "stored password hash exceeds verification policy")
	}
	matched := false
	err = h.gate.Execute(ctx, func(op context.Context) error {
		if err := op.Err(); err != nil {
			return err
		}
		input := []byte(plain.value.Reveal())
		defer clear(input)
		p := parsed.parameters
		key := argon2.IDKey(input, parsed.salt, p.Iterations, p.MemoryKiB, p.Parallelism, uint32(len(parsed.key)))
		defer clear(key)
		matched = subtle.ConstantTimeCompare(key, parsed.key) == 1
		return nil
	})
	if err != nil {
		return false, err
	}
	return matched, nil
}

// NeedsRehash reports divergence from this explicit issuance policy; it does
// not authenticate the input or write a replacement. Call only after successful
// Check and update using a compare-and-swap on the original stored hash. Changing
// policy may increase or decrease costs; choose it deliberately for deployment.
func (h *Hasher) NeedsRehash(hash Hash) (bool, error) {
	if err := h.Validate(); err != nil {
		return false, err
	}
	parsed, err := parse(hash.encoded.Reveal())
	if err != nil {
		return false, err
	}
	return parsed.parameters != h.config.Parameters || len(parsed.salt) != SaltBytes || len(parsed.key) != KeyBytes, nil
}
