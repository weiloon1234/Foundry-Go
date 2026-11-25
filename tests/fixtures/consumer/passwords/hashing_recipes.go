package passwords

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
)

// doc-example:begin hash-and-check
func hashAndCheck(ctx context.Context, submittedPassword string) (bool, error) {
	hasher, err := password.New(password.DefaultConfig())
	if err != nil {
		return false, err
	}
	plain, err := password.NewPlaintext(secret.New(submittedPassword))
	if err != nil {
		return false, err
	}
	// Apply the application's password-strength rules before storing a new hash.
	stored, err := hasher.Hash(ctx, plain)
	if err != nil {
		return false, err
	}
	return hasher.Check(ctx, plain, stored)
}

// doc-example:end hash-and-check

// doc-example:begin password-result
func passwordResult(ctx context.Context, login *auth.PasswordLogin[Account, model.ID[Account], string], email string, request LoginRequest) (Account, auth.Proof[Account, model.ID[Account]], error) {
	result, err := login.Authenticate(ctx, email, request.Password)
	if err != nil {
		return Account{}, auth.Proof[Account, model.ID[Account]]{}, err
	}
	account := result.Subject() // Already resolved concrete model.
	proof := result.Proof()     // Model/key types retained.
	// Pass proof to session/token issuance using its matching assurance mode.
	return account, proof, nil
}

// doc-example:end password-result
