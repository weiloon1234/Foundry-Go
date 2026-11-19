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

	enrollEndpointRoute := route("mfa.enroll", "/mfa/enroll")
	enrollEndpoint := foundryhttp.DefineEndpoint(enrollEndpointRoute, foundryhttp.EmptyQuery(), EnrollmentBody(), foundryhttp.MFAEnrollmentResponse[Account](201, source))

	enroll := foundryhttp.ConfigureEndpoint(enrollEndpoint, foundryhttp.Use(browser.Middleware())).Handle(func(ctx context.Context, input EnrollmentInput) (mfa.Enrollment[Account], error) {
		password, err := verify(ctx, input.Body.Credentials)
		if err != nil {
			return mfa.Enrollment[Account]{}, err
		}
		return factors.Enroll(ctx, password)
	})

	confirmEndpointRoute := route("mfa.confirm", "/mfa/confirm")
	confirmEndpoint := foundryhttp.DefineEndpoint(confirmEndpointRoute, foundryhttp.EmptyQuery(), ConfirmationBody(), foundryhttp.MFARecoveryResponse[Account](200))

	confirm := foundryhttp.ConfigureEndpoint(confirmEndpoint, foundryhttp.Use(browser.Middleware())).Handle(func(ctx context.Context, input ConfirmationInput) (mfa.RecoveryCodes[Account], error) {
		password, err := verify(ctx, input.Body.Credentials)
		if err != nil {
			return mfa.RecoveryCodes[Account]{}, err
		}
		return factors.Confirm(ctx, password, input.Body.EnrollmentID, input.Body.Code)
	})

	regenerateEndpointRoute := route("mfa.regenerate", "/mfa/recovery")
	regenerateEndpoint := foundryhttp.DefineEndpoint(regenerateEndpointRoute, foundryhttp.EmptyQuery(), RegenerationBody(), foundryhttp.MFARecoveryResponse[Account](200))

	regenerate := foundryhttp.ConfigureEndpoint(regenerateEndpoint, foundryhttp.Use(browser.Middleware())).Handle(func(ctx context.Context, input RegenerationInput) (mfa.RecoveryCodes[Account], error) {
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

	webTOTPEndpointRoute := route("mfa.web.totp", "/mfa/web/totp")
	webTOTPEndpoint := foundryhttp.DefineEndpoint(webTOTPEndpointRoute, foundryhttp.EmptyQuery(), TOTPBody(), foundryhttp.EmptyResponse(204))

	webTOTP := foundryhttp.ConfigureEndpoint(webTOTPEndpoint, foundryhttp.Use(browser.Middleware())).Handle(func(ctx context.Context, input TOTPInput) (foundryhttp.NoContent, error) {
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

	webRecoveryEndpointRoute := route("mfa.web.recovery", "/mfa/web/recovery")
	webRecoveryEndpoint := foundryhttp.DefineEndpoint(webRecoveryEndpointRoute, foundryhttp.EmptyQuery(), RecoveryBody(), foundryhttp.EmptyResponse(204))

	webRecovery := foundryhttp.ConfigureEndpoint(webRecoveryEndpoint, foundryhttp.Use(browser.Middleware())).Handle(func(ctx context.Context, input RecoveryInput) (foundryhttp.NoContent, error) {
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

	apiTOTPRoute := route("mfa.api.totp", "/mfa/api/totp")

	apiTOTP := foundryhttp.DefineEndpoint(apiTOTPRoute, foundryhttp.EmptyQuery(), foundryhttp.MFATOTPBody(), tokenResponse).Handle(func(ctx context.Context, input TokenTOTPInput) (IssuedToken, error) {
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

	apiRecoveryRoute := route("mfa.api.recovery", "/mfa/api/recovery")

	apiRecovery := foundryhttp.DefineEndpoint(apiRecoveryRoute, foundryhttp.EmptyQuery(), foundryhttp.MFARecoveryBody(), tokenResponse).Handle(func(ctx context.Context, input TokenRecoveryInput) (IssuedToken, error) {
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
