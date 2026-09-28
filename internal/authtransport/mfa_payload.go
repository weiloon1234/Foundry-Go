package authtransport

import (
	"fmt"

	"github.com/weiloon1234/Foundry-Go/auth/mfa"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

//foundry:dto
type MFAEnrollmentResponse struct {
	EnrollmentID    model.ID[mfa.Record] `json:"enrollment_id"`
	Secret          string               `json:"secret"`
	ProvisioningURI string               `json:"provisioning_uri"`
	ExpiresAt       temporal.DateTime    `json:"expires_at"`
}

//foundry:dto
type MFARecoveryResponse struct {
	RecoveryCodes []string `json:"recovery_codes"`
}

// Only credential response adapters construct these intentionally disclosing DTOs.
func (MFAEnrollmentResponse) Format(s fmt.State, _ rune) {
	_, _ = s.Write([]byte("MFA enrollment response"))
}
func (MFARecoveryResponse) Format(s fmt.State, _ rune) {
	_, _ = s.Write([]byte("MFA recovery response"))
}
