package mfa

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
)

// MaxRotationBatch bounds the factors examined by one ReencryptStale call.
const MaxRotationBatch = 256

// StoredCiphertext is one stored factor envelope selected for key rotation. It
// carries the opaque subject key and factor generation that bind the envelope's
// encryption context, never a model, identity or plaintext.
type StoredCiphertext struct {
	Owner      string
	Generation model.ID[Record]
	Ciphertext encryption.Ciphertext
}

// RotationBackend is the storage contract for password-free key rotation across
// every address sharing the store. Implementations never load domain models.
type RotationBackend interface {
	// StaleCiphertexts returns at most limit factors whose envelope was not
	// produced by the active key, ordered by Owner and strictly after the
	// cursor (empty starts from the beginning).
	StaleCiphertexts(ctx context.Context, active encryption.KeyID, after string, limit int) ([]StoredCiphertext, error)
	// ReplaceCiphertext stores next only while the factor still has the same
	// owner, generation and envelope. False means it changed concurrently.
	ReplaceCiphertext(ctx context.Context, stored StoredCiphertext, next encryption.Ciphertext) (bool, error)
}

// RotationCursor resumes rotation after the last examined factor. The zero
// value starts from the beginning. It is opaque and not a model reference.
type RotationCursor struct{ owner string }

func (c RotationCursor) IsZero() bool { return c.owner == "" }

// Rotation reports one bounded batch. Reencrypted counts committed
// replacements; Changed counts factors that changed concurrently (a later run
// re-examines them if still stale); Failed counts envelopes that could not be
// decrypted with the retained keys. Done reports that no stale factor remained
// after Next when the batch was read.
type Rotation struct {
	Reencrypted uint64
	Changed     uint64
	Failed      uint64
	Next        RotationCursor
	Done        bool
}

// ReencryptStale re-encrypts up to limit stored factors under the keyring's
// active key, for key rotation, without any user's password or domain model.
// Each replacement is conditional on the factor's generation and envelope, so
// it is safe alongside logins and management. The secret, generation, replay
// step and recovery hashes are unchanged, and no credential is revoked.
// Envelopes whose key is no longer retained are reported as Failed and skipped.
// Counts committed before an error are returned with it.
func (s *Store) ReencryptStale(ctx context.Context, after RotationCursor, limit int) (Rotation, error) {
	if err := s.Validate(); err != nil {
		return Rotation{}, err
	}
	if limit < 1 || limit > MaxRotationBatch {
		return Rotation{}, fault.New(fault.Invalid, "invalid MFA rotation batch size")
	}
	backend, ok := s.backend.(RotationBackend)
	if !ok {
		return Rotation{}, fault.New(fault.Invalid, "MFA backend cannot rotate encryption keys")
	}
	result := Rotation{Next: after}
	err := s.gate.Execute(ctx, func(op context.Context) error {
		stale, err := backend.StaleCiphertexts(op, s.keys.ActiveID(), after.owner, limit)
		if err != nil {
			return err
		}
		if len(stale) > limit {
			return fault.New(fault.Invalid, "MFA rotation listing exceeded its limit")
		}
		for _, item := range stale {
			if item.Owner <= result.Next.owner || item.Generation.IsZero() || item.Ciphertext.IsZero() {
				return fault.New(fault.Invalid, "MFA rotation listing is unordered or incomplete")
			}
			result.Next = RotationCursor{owner: item.Owner}
			binding, err := factorContext(item.Owner, item.Generation)
			if err != nil {
				return err
			}
			next, err := s.keys.Reencrypt(op, binding, item.Ciphertext)
			if err != nil {
				if canceled := op.Err(); canceled != nil {
					return canceled
				}
				result.Failed++
				continue
			}
			replaced, err := backend.ReplaceCiphertext(op, item, next)
			if err != nil {
				return err
			}
			if replaced {
				result.Reencrypted++
			} else {
				result.Changed++
			}
		}
		result.Done = len(stale) < limit
		return nil
	})
	return result, err
}
