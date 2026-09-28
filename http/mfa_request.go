package http

import "github.com/weiloon1234/Foundry-Go/internal/authtransport"

// MFATOTPRequest carries a pending token and a typed TOTP response. Completion
// verifies both; a syntactically valid Challenge grants no authority.
type MFATOTPRequest = authtransport.MFATOTPRequest

// MFARecoveryRequest chooses a recovery code instead of a TOTP code.
type MFARecoveryRequest = authtransport.MFARecoveryRequest

// MFAChallengeCredential retains redaction until its explicit Secret boundary.
type MFAChallengeCredential = authtransport.MFAChallengeCredential

// MFATOTPBody uses generated bounded JSON decoding: required fields, duplicate
// and unknown names, nulls and invalid credential/code syntax are rejected.
func MFATOTPBody() Body[MFATOTPRequest] { return JSONBody(authtransport.MFATOTPRequestJSON()) }
func MFARecoveryBody() Body[MFARecoveryRequest] {
	return JSONBody(authtransport.MFARecoveryRequestJSON())
}
