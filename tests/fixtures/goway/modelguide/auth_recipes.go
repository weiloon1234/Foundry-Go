package modelguide

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth/emailverification"
	"github.com/weiloon1234/Foundry-Go/auth/passwordreset"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/validation"
	databasevalidation "github.com/weiloon1234/Foundry-Go/validation/database"
)

// doc-example:begin account-recovery-issue-reset
func issueReset(ctx context.Context, reset *passwordreset.Reset[Account, int64], account model.Reference[Account, int64]) (passwordreset.Issued[Account], error) {
	return reset.Issue(ctx, account)
}

// doc-example:end account-recovery-issue-reset

// doc-example:begin account-recovery-issue-verification
func issueVerification(ctx context.Context, verification *emailverification.Verification[Account, int64], account model.Reference[Account, int64]) (emailverification.Issued[Account], error) {
	return verification.Issue(ctx, account)
}

// doc-example:end account-recovery-issue-verification

// doc-example:begin sessions-impersonate
func impersonate(ctx context.Context, web *session.Sessions[Account, int64], customer model.Reference[Account, int64]) (session.Issued[Account, int64], error) {
	support, err := session.NewImpersonation(web, web, time.Hour)
	if err != nil {
		return session.Issued[Account, int64]{}, err
	}
	// The caller must authorize the actor against this target before starting.
	return support.Start(ctx, customer, 30*time.Minute)
}

// doc-example:end sessions-impersonate

// doc-example:begin validation-enabled-accounts
func enabledAccounts(db database.Executor, enabled func(context.Context) (bool, error), accounts AccountTable) validation.Rule[int64] {
	lookup := databasevalidation.Scoped(db, func(ctx context.Context) (orm.Query[Account], error) {
		value, err := enabled(ctx)
		return accounts.Query().Where(orm.Eq(accounts.Enabled, value)), err
	}, accounts.Key())
	return validation.Exists(lookup)
}

// doc-example:end validation-enabled-accounts
