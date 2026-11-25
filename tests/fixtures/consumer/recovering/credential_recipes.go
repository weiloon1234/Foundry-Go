package recovering

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	"github.com/weiloon1234/Foundry-Go/model"
)

// doc-example:begin issue-password-session
func issuePasswordSession(ctx context.Context, login *auth.PasswordLogin[Member, model.ID[Member], string], sessions *session.Sessions[Member, model.ID[Member]], email string, passwordInput password.Plaintext) (session.Issued[Member, model.ID[Member]], error) {
	result, err := login.Authenticate(ctx, email, passwordInput)
	if err != nil {
		return session.Issued[Member, model.ID[Member]]{}, err
	}
	// Select the required pending or full assurance flow before issuance.
	return sessions.Issue(ctx, result.Proof(), session.IssueOptions{})
}

// doc-example:end issue-password-session
