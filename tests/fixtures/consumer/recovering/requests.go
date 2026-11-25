package recovering

import (
	"github.com/weiloon1234/Foundry-Go/auth/emailverification"
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/auth/passwordreset"
)

// doc-example:begin recovery-request-inputs
//
//foundry:dto http=body
type ResetRequest struct {
	Token    passwordreset.Token[Member] `json:"token"`
	Password password.Plaintext          `json:"password" client:"kind=password"`
}

//foundry:dto http=body
type VerificationRequest struct {
	Token emailverification.Token[Member] `json:"token"`
}

// doc-example:end recovery-request-inputs

//foundry:dto http=body
type LinkRequest struct {
	Email string `json:"email"`
}
