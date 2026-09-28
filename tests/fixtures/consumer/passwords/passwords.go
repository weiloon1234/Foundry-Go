// Package passwords demonstrates typed hashing and a redacted login DTO.
package passwords

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/auth/password"
)

//foundry:dto
type LoginRequest struct {
	Email    string             `json:"email"`
	Password password.Plaintext `json:"password"`
}

// HashForRegistration returns a hash, never a plaintext string. Persistence
// and authenticated proofs are separate explicit boundaries.
func HashForRegistration(ctx context.Context, hasher *password.Hasher, input LoginRequest) (password.Hash, error) {
	return hasher.Hash(ctx, input.Password)
}
func CheckPassword(ctx context.Context, hasher *password.Hasher, input LoginRequest, stored password.Hash) (bool, error) {
	return hasher.Check(ctx, input.Password, stored)
}
func ChangedHashPolicy(hasher *password.Hasher, stored password.Hash) (bool, error) {
	return hasher.NeedsRehash(stored)
}
