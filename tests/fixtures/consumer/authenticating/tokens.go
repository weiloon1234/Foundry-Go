package authenticating

import (
	"context"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	tokenpg "github.com/weiloon1234/Foundry-Go/auth/token/postgres"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
)

type UserTokens = token.Tokens[models.User, model.ID[models.User]]
type UserTokenInfo = token.Info[models.User, model.ID[models.User]]
type IssuedUserToken = token.Issued[models.User, model.ID[models.User]]

// APITokens borrows the consumer's existing pool and stored-model provider.
// Apply tokenpg.Migrations explicitly before running token operations.
func APITokens(db *database.DB, provider UserProvider, persistence tokenpg.Config, config token.Config) (*UserTokens, error) {
	backend, err := tokenpg.New(db, persistence)
	if err != nil {
		return nil, err
	}
	store, err := token.NewStore(backend, config)
	if err != nil {
		return nil, err
	}
	declared, err := OrderReadScopes()
	if err != nil {
		return nil, err
	}
	return token.New(store, "users.tokens", provider, "users.bearer", declared)
}

// StartVerifiedToken receives a proof from the trusted login flow. It never
// constructs proof from a submitted ID or accepts raw permission strings.
func StartVerifiedToken(ctx context.Context, tokens *UserTokens, proof auth.Proof[models.User, model.ID[models.User]], name string, renewable bool) (IssuedUserToken, error) {
	granted, err := OrderReadScopes()
	if err != nil {
		return IssuedUserToken{}, err
	}
	return tokens.Issue(ctx, proof, token.IssueOptions[models.User]{Name: name, Scopes: granted, Refresh: renewable})
}
func RefreshVerifiedToken(ctx context.Context, tokens *UserTokens, raw secret.String) (IssuedUserToken, error) {
	return tokens.Refresh(ctx, raw)
}
func UserTokenSubject(info UserTokenInfo) model.Reference[models.User, model.ID[models.User]] {
	return info.Subject()
}
func ListUserTokens(ctx context.Context, tokens *UserTokens, user models.User) ([]UserTokenInfo, error) {
	return tokens.List(ctx, user.FoundryReference())
}
