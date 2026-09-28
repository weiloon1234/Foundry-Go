// Package randomtoken supplies bounded cryptographically random opaque values.
// Encoded tokens use secret.String; revealing one for transmission is explicit.
package randomtoken

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
)

const MaxBytes = 4096
const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
const rejectionCutoff = 256 / len(alphabet) * len(alphabet)

// Bytes returns independent entropy, with size in [1, MaxBytes]. Unlike encoded
// tokens these raw bytes have no automatic formatting redaction.
func Bytes(size int) ([]byte, error) {
	if size < 1 || size > MaxBytes {
		return nil, invalid()
	}
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return nil, fault.New(fault.Internal, "random token generation failed")
	}
	return data, nil
}

func Hex(size int) (secret.String, error) {
	data, err := Bytes(size)
	if err != nil {
		return secret.String{}, err
	}
	return secret.New(hex.EncodeToString(data)), nil
}

// Base64 encodes size random bytes using unpadded URL-safe base64.
func Base64(size int) (secret.String, error) {
	data, err := Bytes(size)
	if err != nil {
		return secret.String{}, err
	}
	return secret.New(base64.RawURLEncoding.EncodeToString(data)), nil
}

// Generate produces exactly length alphanumeric characters. Rejection sampling
// avoids modulo bias. Length is a character count, not a number of entropy bytes.
func Generate(length int) (secret.String, error) {
	if length < 1 || length > MaxBytes {
		return secret.String{}, invalid()
	}
	result := make([]byte, 0, length)
	for len(result) < length {
		data, err := Bytes(64)
		if err != nil {
			return secret.String{}, err
		}
		for _, b := range data {
			if int(b) >= rejectionCutoff {
				continue
			}
			result = append(result, alphabet[int(b)%len(alphabet)])
			if len(result) == length {
				break
			}
		}
	}
	return secret.New(string(result)), nil
}
func invalid() error { return fault.New(fault.Invalid, "random token length is outside its bound") }
