package multifactor

import (
	"github.com/weiloon1234/Foundry-Go/auth/mfa"
	"github.com/weiloon1234/Foundry-Go/auth/password"
)

// PasswordCredentials is reverified for each factor-management operation.
// It is never accepted as an authenticated model or reusable reauth ticket.
type PasswordCredentials struct {
	Email    string             `json:"email"`
	Password password.Plaintext `json:"password" client:"kind=password"`
}

//foundry:dto http=body
type EnrollmentRequest struct {
	Credentials PasswordCredentials `json:"credentials"`
}

//foundry:dto http=body
type ConfirmationRequest struct {
	Credentials  PasswordCredentials       `json:"credentials"`
	EnrollmentID mfa.EnrollmentID[Account] `json:"enrollment_id"`
	Code         mfa.TOTPCode              `json:"code"`
}

//foundry:dto http=body
type RegenerationRequest struct {
	Credentials PasswordCredentials `json:"credentials"`
	Code        mfa.TOTPCode        `json:"code"`
}
