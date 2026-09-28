// Package multifactor exercises typed factor inputs and authenticated encryption
// from an independent consumer. Routes exercise enrollment and login completion.
package multifactor

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth/mfa"
	"github.com/weiloon1234/Foundry-Go/encryption"
)

// Enrollment preserves a typed TOTP secret until the encryption boundary. The
// owner context comes from trusted record identity, never a submitted owner.
func EncryptEnrollment(ctx context.Context, keys *encryption.Keyring, owner encryption.Context, factor mfa.TOTPSecret) (encryption.Ciphertext, error) {
	if err := factor.Validate(); err != nil {
		return encryption.Ciphertext{}, err
	}
	return keys.Encrypt(ctx, owner, factor.Secret())
}
func DecryptEnrollment(ctx context.Context, keys *encryption.Keyring, owner encryption.Context, ciphertext encryption.Ciphertext) (mfa.TOTPSecret, error) {
	plain, err := keys.Decrypt(ctx, owner, ciphertext)
	if err != nil {
		return mfa.TOTPSecret{}, err
	}
	return mfa.ParseTOTPSecret(plain)
}
func RotateEncryption(ctx context.Context, keys *encryption.Keyring, owner encryption.Context, ciphertext encryption.Ciphertext) (encryption.Ciphertext, error) {
	return keys.Reencrypt(ctx, owner, ciphertext)
}

//foundry:dto
type TOTPRequest struct {
	Code mfa.TOTPCode `json:"code"`
}

//foundry:dto
type RecoveryRequest struct {
	Code mfa.RecoveryCode `json:"code"`
}
