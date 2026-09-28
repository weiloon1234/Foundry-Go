package recovering

import (
	"context"
	"log/slog"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/challenge"
	"github.com/weiloon1234/Foundry-Go/auth/emailverification"
	"github.com/weiloon1234/Foundry-Go/auth/passwordreset"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
	"github.com/weiloon1234/Foundry-Go/value"
)

type ResetRequests = challenge.Requests[Member, model.ID[Member], string, challenge.PasswordReset]
type VerificationRequests = challenge.Requests[Member, model.ID[Member], string, challenge.EmailVerification]
type RecipientLookup func(context.Context, string) (value.Optional[model.Reference[Member, model.ID[Member]]], error)

// Delivery callbacks must address issued.Subject().Email. The submitted lookup
// string is deliberately absent from the delivery callback's parameters.
func NewResetRequests(reset *Reset, limiter ratelimit.Limiter[string], lookup RecipientLookup, deliver func(context.Context, passwordreset.Issued[Member]) error, logger *slog.Logger) (*ResetRequests, error) {
	return challenge.NewRequests(reset, limiter, challenge.RequestCallbacks[Member, model.ID[Member], string, challenge.PasswordReset]{Lookup: lookup, Deliver: deliver}, logger, auth.DefaultConfig())
}
func NewVerificationRequests(verification *Verification, limiter ratelimit.Limiter[string], lookup RecipientLookup, deliver func(context.Context, emailverification.Issued[Member]) error, logger *slog.Logger) (*VerificationRequests, error) {
	return challenge.NewRequests(verification, limiter, challenge.RequestCallbacks[Member, model.ID[Member], string, challenge.EmailVerification]{Lookup: lookup, Deliver: deliver}, logger, auth.DefaultConfig())
}

// RequestLinks shows the generic outward contract. Neither function returns a
// model or link. Validate/canonicalize submittedEmail and apply ingress rate limits
// before calling this boundary; the same value owns recipient quota and lookup.
func RequestReset(ctx context.Context, requests *ResetRequests, submittedEmail string) error {
	return requests.Request(ctx, submittedEmail)
}
func RequestVerification(ctx context.Context, requests *VerificationRequests, submittedEmail string) error {
	return requests.Request(ctx, submittedEmail)
}
