package multifactor

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth/mfa"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
)

type Sessions = session.Sessions[Account, model.ID[Account]]
type Tokens = token.Tokens[Account, model.ID[Account]]

// CompleteSession keeps pending credentials bound to the original web guard.
// Transport must publish the returned credential through its protected boundary.
func CompleteSession(ctx context.Context, factors *Factors, sessions *Sessions, pending secret.String, request TOTPRequest) (session.Issued[Account, model.ID[Account]], error) {
	response, err := mfa.TOTPResponse(request.Code)
	if err != nil {
		return session.Issued[Account, model.ID[Account]]{}, err
	}
	verifier, err := factors.Verifier(response)
	if err != nil {
		return session.Issued[Account, model.ID[Account]]{}, err
	}
	return sessions.CompleteMFA(ctx, pending, verifier, session.IssueOptions{})
}

// CompleteToken selects options from server login policy. Client payloads do not
// select arbitrary scopes; the binding's declared ceiling is enforced as usual.
func CompleteToken(ctx context.Context, factors *Factors, tokens *Tokens, pending secret.String, request RecoveryRequest) (token.Issued[Account, model.ID[Account]], error) {
	response, err := mfa.RecoveryResponse(request.Code)
	if err != nil {
		return token.Issued[Account, model.ID[Account]]{}, err
	}
	verifier, err := factors.Verifier(response)
	if err != nil {
		return token.Issued[Account, model.ID[Account]]{}, err
	}
	return tokens.CompleteMFA(ctx, pending, verifier, token.IssueOptions[Account]{Name: "Browser API", Refresh: true})
}
