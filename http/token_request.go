package http

import "github.com/weiloon1234/Foundry-Go/internal/authtransport"

// RefreshTokenRequest is Foundry's refresh input. RefreshToken retains a
// redacted credential; pass its Secret() directly to Tokens.Refresh. It comes
// from the JSON body (RefreshTokenBody) or, for browser clients, only from the
// HttpOnly refresh cookie (RefreshTokenCookie); never from URL parameters.
type RefreshTokenRequest = authtransport.RefreshRequest

// RefreshCredential is the validated, redacted value in RefreshTokenRequest.
// Secret() explicitly permits using it for a token operation.
type RefreshCredential = authtransport.RefreshCredential

// RefreshTokenBody uses the same generated DTO metadata as contract export.
// Unknown, duplicate, missing, null and malformed credential fields are rejected
// by the ordinary bounded JSON decoder before a refresh handler runs.
func RefreshTokenBody() Body[RefreshTokenRequest] {
	return JSONBody(authtransport.RefreshRequestJSON())
}
