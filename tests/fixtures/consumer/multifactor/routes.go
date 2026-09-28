package multifactor

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/mfa"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/clock"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
)

type Browser = foundryhttp.BrowserSessions[Account, model.ID[Account]]
type PasswordVerifier func(context.Context, PasswordCredentials) (auth.PasswordResult[Account, model.ID[Account]], error)
type EnrollmentInput = foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, EnrollmentRequest]
type ConfirmationInput = foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, ConfirmationRequest]
type RegenerationInput = foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, RegenerationRequest]
type TOTPInput = foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, TOTPRequest]
type RecoveryInput = foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, RecoveryRequest]
type TokenTOTPInput = foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.MFATOTPRequest]
type TokenRecoveryInput = foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.MFARecoveryRequest]
type IssuedToken = token.Issued[Account, model.ID[Account]]

// MFARoutes demonstrates framework-owned disclosure and cookie staging. Public
// challenge routes verify the submitted password or pending credential explicitly;
// ordinary guarded endpoints cannot accept PendingMFA. Add application ingress
// rate limiting before this router; factor verification also uses its own throttle.
func MFARoutes(factors *Factors, browser *Browser, tokens *Tokens, source clock.Clock, verify PasswordVerifier) (*foundryhttp.Router, error) {
	route := func(id foundryhttp.RouteID, path string) foundryhttp.Route[foundryhttp.NoPath] {
		return foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: id, Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath(path))
	}
	enroll := foundryhttp.DefineEndpoint(route("mfa.enroll", "/mfa/enroll"), foundryhttp.EmptyQuery(), foundryhttp.JSONBody(EnrollmentRequestJSON()), foundryhttp.MFAEnrollmentResponse[Account](201, source)).
		WithMiddleware(browser.Middleware()).Handle(func(ctx context.Context, input EnrollmentInput) (mfa.Enrollment[Account], error) {
		password, err := verify(ctx, input.Body.Credentials)
		if err != nil {
			return mfa.Enrollment[Account]{}, err
		}
		return factors.Enroll(ctx, password)
	})
	confirm := foundryhttp.DefineEndpoint(route("mfa.confirm", "/mfa/confirm"), foundryhttp.EmptyQuery(), foundryhttp.JSONBody(ConfirmationRequestJSON()), foundryhttp.MFARecoveryResponse[Account](200)).
		WithMiddleware(browser.Middleware()).Handle(func(ctx context.Context, input ConfirmationInput) (mfa.RecoveryCodes[Account], error) {
		password, err := verify(ctx, input.Body.Credentials)
		if err != nil {
			return mfa.RecoveryCodes[Account]{}, err
		}
		return factors.Confirm(ctx, password, input.Body.EnrollmentID, input.Body.Code)
	})
	regenerate := foundryhttp.DefineEndpoint(route("mfa.regenerate", "/mfa/recovery"), foundryhttp.EmptyQuery(), foundryhttp.JSONBody(RegenerationRequestJSON()), foundryhttp.MFARecoveryResponse[Account](200)).
		WithMiddleware(browser.Middleware()).Handle(func(ctx context.Context, input RegenerationInput) (mfa.RecoveryCodes[Account], error) {
		password, err := verify(ctx, input.Body.Credentials)
		if err != nil {
			return mfa.RecoveryCodes[Account]{}, err
		}
		response, err := mfa.TOTPResponse(input.Body.Code)
		if err != nil {
			return mfa.RecoveryCodes[Account]{}, err
		}
		return factors.RegenerateRecovery(ctx, password, response)
	})
	webTOTP := foundryhttp.DefineEndpoint(route("mfa.web.totp", "/mfa/web/totp"), foundryhttp.EmptyQuery(), foundryhttp.JSONBody(TOTPRequestJSON()), foundryhttp.EmptyResponse(204)).
		WithMiddleware(browser.Middleware()).Handle(func(ctx context.Context, input TOTPInput) (foundryhttp.NoContent, error) {
		response, err := mfa.TOTPResponse(input.Body.Code)
		if err != nil {
			return foundryhttp.NoContent{}, err
		}
		factor, err := factors.Verifier(response)
		if err != nil {
			return foundryhttp.NoContent{}, err
		}
		_, err = browser.CompleteMFA(ctx, factor, session.IssueOptions{})
		return foundryhttp.NoContent{}, err
	})
	webRecovery := foundryhttp.DefineEndpoint(route("mfa.web.recovery", "/mfa/web/recovery"), foundryhttp.EmptyQuery(), foundryhttp.JSONBody(RecoveryRequestJSON()), foundryhttp.EmptyResponse(204)).
		WithMiddleware(browser.Middleware()).Handle(func(ctx context.Context, input RecoveryInput) (foundryhttp.NoContent, error) {
		response, err := mfa.RecoveryResponse(input.Body.Code)
		if err != nil {
			return foundryhttp.NoContent{}, err
		}
		factor, err := factors.Verifier(response)
		if err != nil {
			return foundryhttp.NoContent{}, err
		}
		_, err = browser.CompleteMFA(ctx, factor, session.IssueOptions{})
		return foundryhttp.NoContent{}, err
	})
	tokenResponse := foundryhttp.TokenResponse[Account, model.ID[Account]](200, source)
	apiTOTP := foundryhttp.DefineEndpoint(route("mfa.api.totp", "/mfa/api/totp"), foundryhttp.EmptyQuery(), foundryhttp.MFATOTPBody(), tokenResponse).
		Handle(func(ctx context.Context, input TokenTOTPInput) (IssuedToken, error) {
			response, err := mfa.TOTPResponse(input.Body.Code)
			if err != nil {
				return IssuedToken{}, err
			}
			factor, err := factors.Verifier(response)
			if err != nil {
				return IssuedToken{}, err
			}
			return tokens.CompleteMFA(ctx, input.Body.Challenge.Secret(), factor, token.IssueOptions[Account]{Name: "API", Refresh: true})
		})
	apiRecovery := foundryhttp.DefineEndpoint(route("mfa.api.recovery", "/mfa/api/recovery"), foundryhttp.EmptyQuery(), foundryhttp.MFARecoveryBody(), tokenResponse).
		Handle(func(ctx context.Context, input TokenRecoveryInput) (IssuedToken, error) {
			response, err := mfa.RecoveryResponse(input.Body.Code)
			if err != nil {
				return IssuedToken{}, err
			}
			factor, err := factors.Verifier(response)
			if err != nil {
				return IssuedToken{}, err
			}
			return tokens.CompleteMFA(ctx, input.Body.Challenge.Secret(), factor, token.IssueOptions[Account]{Name: "API", Refresh: true})
		})
	return foundryhttp.NewRouter(enroll, confirm, regenerate, webTOTP, webRecovery, apiTOTP, apiRecovery)
}
