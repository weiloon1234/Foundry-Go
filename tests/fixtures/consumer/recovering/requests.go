package recovering

import (
	"github.com/weiloon1234/Foundry-Go/auth/emailverification"
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/auth/passwordreset"
)

//foundry:dto
type ResetRequest struct {
	Token    passwordreset.Token[Member] `json:"token"`
	Password password.Plaintext          `json:"password"`
}

//foundry:dto
type VerificationRequest struct {
	Token emailverification.Token[Member] `json:"token"`
}

//foundry:dto
type LinkRequest struct {
	Email string `json:"email"`
}
