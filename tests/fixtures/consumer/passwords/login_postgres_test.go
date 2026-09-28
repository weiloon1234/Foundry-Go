package passwords_test

import (
	"context"
	"errors"
	"testing"

	"foundry.test/consumer/passwords"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/secret"
)

func TestPostgresModelFirstPasswordLoginAndRehash(t *testing.T) {
	passwordModels(t, func(tx *database.Tx) error {
		config := password.DefaultConfig()
		config.Parameters = password.Parameters{MemoryKiB: 19 * 1024, Iterations: 2, Parallelism: 1}
		oldHasher, err := password.New(config)
		if err != nil {
			return err
		}
		plain, err := password.NewPlaintext(secret.New("exact input"))
		if err != nil {
			return err
		}
		request := passwords.LoginRequest{Email: "member@example.test", Password: plain}
		account, err := passwords.CreateAccount(t.Context(), tx, oldHasher, request)
		if err != nil {
			return err
		}
		config.Parameters.Iterations++
		hasher, err := password.New(config)
		if err != nil {
			return err
		}
		requireMFA := false
		login, err := passwords.NewLogin(tx, passwords.Accounts(tx), hasher, func(context.Context, passwords.Account) (bool, error) { return requireMFA, nil })
		if err != nil {
			return err
		}
		result, err := passwords.Authenticate(t.Context(), login, request)
		if err != nil {
			return err
		}
		if result.Subject().ID != account.ID || result.Subject().Digest == account.Digest || result.Proof().Assurance() != auth.Authenticated {
			return errors.New("login lost model or rehash result")
		}
		current, err := passwords.QueryPasswordAccounts().RequireFind(t.Context(), tx, account.ID)
		if err != nil {
			return err
		}
		if current.Digest != result.Subject().Digest {
			return errors.New("proof escaped failed rehash persistence")
		}
		if needs, err := hasher.NeedsRehash(current.Digest); err != nil || needs {
			return errors.New("rehash policy not applied")
		}
		requireMFA = true
		result, err = passwords.Authenticate(t.Context(), login, request)
		if err != nil {
			return err
		}
		if result.Proof().Assurance() != auth.PendingMFA || result.Subject().Digest != current.Digest {
			return errors.New("MFA policy ignored or current hash rewritten")
		}
		if _, err := passwords.QueryPasswordAccounts().Update(t.Context(), tx, account.ID, passwords.AccountDraft{}.SetEnabled(false)); err != nil {
			return err
		}
		result, err = passwords.Authenticate(t.Context(), login, request)
		if !errors.Is(err, auth.Unauthenticated) || !result.Proof().Identity().IsZero() {
			return errors.New("disabled model authenticated")
		}
		return nil
	})
}
