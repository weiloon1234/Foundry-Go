package http

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/authtransport"
	"github.com/weiloon1234/Foundry-Go/value"
)

// TokenResponse delivers a model-owned issuance through an explicit credential
// boundary. Handlers return token.Issued directly. Ordinary JSON of Issued stays
// redacted. The endpoint requires POST and TLS (or a configured TrustedProxy),
// disables caching, and checks access expiry before preparing its bounded JSON.
// The clock must agree with the token backend. Construction performs no I/O.
// A committed credential cannot be atomically delivered over HTTP: failed or
// uncertain delivery must not retry issuance/refresh automatically.
func TokenResponse[M, K any](status int, source clock.Clock) Response[token.Issued[M, K]] {
	return credentialResponse(status, authtransport.TokenResponseJSON(), func() error { return credentialClockValid(source) },
		func(_ context.Context, issued token.Issued[M, K]) (authtransport.TokenResponse, error) {
			return tokenPayload(issued, source)
		})
}

func tokenPayload[M, K any](issued token.Issued[M, K], source clock.Clock) (authtransport.TokenResponse, error) {
	var empty authtransport.TokenResponse
	now, err := credentialTime(source)
	if err != nil {
		return empty, err
	}
	info := issued.Info()
	if info.ID().IsZero() || info.Assurance().Validate() != nil || now.UTC().Before(info.IssuedAt().UTC()) || !now.UTC().Before(info.AccessExpiresAt().UTC()) {
		return empty, fault.New(fault.Invalid, "issued token is missing or expired")
	}
	if _, err := token.HashSecret(issued.AccessSecret()); err != nil {
		return empty, err
	}
	refresh, hasRefresh := issued.RefreshSecret().Get()
	if hasRefresh != (info.Mode() == token.Renewable) {
		return empty, fault.New(fault.Invalid, "invalid issued token pair")
	}
	var refreshText value.Optional[string]
	if hasRefresh {
		if _, err := token.HashSecret(refresh); err != nil {
			return empty, err
		}
		refreshText = value.Set(refresh.Reveal())
	}
	return authtransport.TokenResponse{
		Tokens: authtransport.TokenPair{AccessToken: issued.AccessSecret().Reveal(), RefreshToken: refreshText,
			TokenType: "Bearer", ExpiresIn: int64(info.AccessExpiresAt().UTC().Sub(now.UTC()) / time.Second)},
		MFARequired: info.Assurance() == auth.PendingMFA,
	}, nil
}
