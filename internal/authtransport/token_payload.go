// Package authtransport owns authentication wire DTOs. Public HTTP adapters
// expose typed operations without allowing implicit credential serialization.
package authtransport

import (
	"fmt"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:dto
type TokenResponse struct {
	Tokens      TokenPair `json:"tokens"`
	MFARequired bool      `json:"mfa_required"`
}

type TokenPair struct {
	AccessToken  string                 `json:"access_token"`
	RefreshToken value.Optional[string] `json:"refresh_token,omitzero"`
	ExpiresIn    int64                  `json:"expires_in"`
	TokenType    string                 `json:"token_type"`
}

// AccessTokenResponse is TokenResponse for the cookie transport: a refresh
// token travels only in its HttpOnly cookie, so the JSON has no refresh field.
//
//foundry:dto
type AccessTokenResponse struct {
	Tokens      AccessTokenPair `json:"tokens"`
	MFARequired bool            `json:"mfa_required"`
}

type AccessTokenPair struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
	TokenType   string `json:"token_type"`
}

func (AccessTokenResponse) Format(s fmt.State, _ rune) {
	_, _ = s.Write([]byte("access token response"))
}
func (AccessTokenPair) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("access token pair")) }

// These internal DTOs intentionally disclose credentials to the JSON encoder.
// Formatting still redacts them; ordinary token.Issued values never serialize.
func (TokenResponse) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("token response")) }
func (TokenPair) Format(s fmt.State, _ rune)     { _, _ = s.Write([]byte("token pair")) }
