package http

import "github.com/weiloon1234/Foundry-Go/internal/authtransport"

// RefreshTokenRequest is Foundry's JSON refresh body. RefreshToken retains a
// redacted credential; pass its Secret() directly to Tokens.Refresh. It never
// comes from cookies or URL parameters. Use RefreshTokenBody for its descriptor.
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
