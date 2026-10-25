package guideadapters_test

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth/emailverification"
	"github.com/weiloon1234/Foundry-Go/auth/passwordreset"
	"github.com/weiloon1234/Foundry-Go/auth/session"
)

// guide:reset-issue
func issueReset(ctx context.Context, reset *passwordreset.Reset[Account, int64], account Account) (passwordreset.Issued[Account], error) {
	return reset.Issue(ctx, AccountIdentity.ReferenceOf(account))
}

// guide:end

// guide:verification-issue
func issueVerification(ctx context.Context, verification *emailverification.Verification[Account, int64], account Account) (emailverification.Issued[Account], error) {
	return verification.Issue(ctx, AccountIdentity.ReferenceOf(account))
}

// guide:end

// guide:impersonation
func impersonate(ctx context.Context, web *session.Sessions[Account, int64], customer Account) (session.Issued[Account, int64], error) {
	support, err := session.NewImpersonation(web, web, time.Hour)
	if err != nil {
		return session.Issued[Account, int64]{}, err
	}
	// The caller must authorize the actor against this target before starting.
	return support.Start(ctx, AccountIdentity.ReferenceOf(customer), 30*time.Minute)
}

// guide:end
