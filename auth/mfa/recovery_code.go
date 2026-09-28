package mfa

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
	"github.com/weiloon1234/Foundry-Go/secret"
)

const MaxRecoveryCodes = 16
const DefaultRecoveryCodes = 10

// RecoveryCode is a distinct canonical 256-bit random factor encoded as 43
// unpadded base64url characters. It is not a password, TOTP code or proof. Only
// the digest is persisted; consumption must be atomic with factor completion.
type RecoveryCode struct{ value secret.String }

func ParseRecoveryCode(raw secret.String) (RecoveryCode, error) {
	if _, err := credential.Hash(raw); err != nil {
		return RecoveryCode{}, err
	}
	return RecoveryCode{value: raw}, nil
}
func (c RecoveryCode) Secret() secret.String      { return c.value }
func (c RecoveryCode) Validate() error            { _, err := ParseRecoveryCode(c.value); return err }
func (RecoveryCode) Format(s fmt.State, _ rune)   { _, _ = s.Write([]byte(secret.Redacted)) }
func (RecoveryCode) LogValue() slog.Value         { return slog.StringValue(secret.Redacted) }
func (RecoveryCode) MarshalJSON() ([]byte, error) { return json.Marshal(secret.Redacted) }
func (c *RecoveryCode) UnmarshalJSON(data []byte) error {
	if c == nil {
		return fault.New(fault.Invalid, "recovery code destination is missing")
	}
	*c = RecoveryCode{}
	var raw string
	if len(data) > 512 {
		return fault.New(fault.Invalid, "invalid recovery code input")
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return fault.New(fault.Invalid, "invalid recovery code input")
	}
	parsed, err := ParseRecoveryCode(secret.New(raw))
	if err != nil {
		return err
	}
	*c = parsed
	return nil
}
func (RecoveryCode) JSONContract() contract.JSON[RecoveryCode] {
	const id contract.TypeID = "github.com/weiloon1234/Foundry-Go/auth/mfa.RecoveryCode"
	return contract.DefineJSONValue[RecoveryCode](contract.Schema{Root: id, Types: []contract.Type{{ID: id, Kind: contract.StringKind}}})
}

// RecoveryHash is a redacted SHA-256 digest of a high-entropy RecoveryCode,
// sharing the credential-hash implementation used by sessions and tokens.
// Encoded is an explicit persistence boundary, never an authentication proof.
type RecoveryHash struct{ digest credential.Digest }

func ParseRecoveryHash(encoded string) (RecoveryHash, error) {
	digest, err := credential.ParseDigest(encoded)
	return RecoveryHash{digest: digest}, err
}
func (h RecoveryHash) Encoded() string            { return h.digest.Hex() }
func (h RecoveryHash) IsZero() bool               { return h.digest.IsZero() }
func (RecoveryHash) Format(s fmt.State, _ rune)   { _, _ = s.Write([]byte(secret.Redacted)) }
func (RecoveryHash) LogValue() slog.Value         { return slog.StringValue(secret.Redacted) }
func (RecoveryHash) MarshalJSON() ([]byte, error) { return json.Marshal(secret.Redacted) }

func newRecoveryCodes(count int) ([]RecoveryCode, []RecoveryHash, error) {
	if count < 1 || count > MaxRecoveryCodes {
		return nil, nil, fault.New(fault.Invalid, "invalid recovery code count")
	}
	codes := make([]RecoveryCode, 0, count)
	hashes := make([]RecoveryHash, 0, count)
	for range count {
		raw, digest, err := credential.New()
		if err != nil {
			return nil, nil, err
		}
		for _, prior := range hashes {
			if prior.digest.Equal(digest) {
				return nil, nil, fault.New(fault.Internal, "recovery code generation collision")
			}
		}
		codes = append(codes, RecoveryCode{value: raw})
		hashes = append(hashes, RecoveryHash{digest: digest})
	}
	return codes, hashes, nil
}

// consumeRecovery returns owned state; it never changes the caller's snapshot.
// A successful match becomes single-use only when persisted under the factor
// lock in the same transaction as the authorized action.
func consumeRecovery(hashes []RecoveryHash, code RecoveryCode) ([]RecoveryHash, bool, error) {
	if err := validateRecoveryHashes(hashes); err != nil {
		return nil, false, err
	}
	digest, err := credential.Hash(code.Secret())
	if err != nil {
		return nil, false, err
	}
	selected := -1
	for i, hash := range hashes {
		if hash.digest.Equal(digest) {
			selected = i
		}
	}
	if selected < 0 {
		return nil, false, nil
	}
	result := make([]RecoveryHash, 0, len(hashes)-1)
	result = append(result, hashes[:selected]...)
	result = append(result, hashes[selected+1:]...)
	return result, true, nil
}
func validateRecoveryHashes(hashes []RecoveryHash) error {
	if len(hashes) > MaxRecoveryCodes {
		return fault.New(fault.Invalid, "recovery hash capacity exceeded")
	}
	for i, hash := range hashes {
		if hash.IsZero() {
			return fault.New(fault.Invalid, "invalid recovery hash")
		}
		for _, prior := range hashes[:i] {
			if prior.digest.Equal(hash.digest) {
				return fault.New(fault.Duplicate, "duplicate recovery hash")
			}
		}
	}
	return nil
}
