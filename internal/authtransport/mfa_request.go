package authtransport

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/weiloon1234/Foundry-Go/auth/mfa"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
)

//foundry:dto
type MFATOTPRequest struct {
	Challenge MFAChallengeCredential `json:"challenge"`
	Code      mfa.TOTPCode           `json:"code"`
}

//foundry:dto
type MFARecoveryRequest struct {
	Challenge MFAChallengeCredential `json:"challenge"`
	Code      mfa.RecoveryCode       `json:"code"`
}

// MFAChallengeCredential is unverified JSON input for token completion, distinct
// from a refresh credential. Its syntax does not establish PendingMFA assurance.
type MFAChallengeCredential struct{ secret secret.String }

func (v MFAChallengeCredential) Secret() secret.String      { return v.secret }
func (MFAChallengeCredential) Format(s fmt.State, _ rune)   { _, _ = s.Write([]byte(secret.Redacted)) }
func (MFAChallengeCredential) LogValue() slog.Value         { return slog.StringValue(secret.Redacted) }
func (MFAChallengeCredential) MarshalJSON() ([]byte, error) { return json.Marshal(secret.Redacted) }
func (v *MFAChallengeCredential) UnmarshalJSON(data []byte) error {
	if v == nil {
		return fault.New(fault.Invalid, "MFA challenge destination is missing")
	}
	*v = MFAChallengeCredential{}
	candidate, err := parseTokenCredential(data)
	if err != nil {
		return err
	}
	v.secret = candidate
	return nil
}
func (MFAChallengeCredential) JSONContract() contract.JSON[MFAChallengeCredential] {
	const id contract.TypeID = "github.com/weiloon1234/Foundry-Go/internal/authtransport.MFAChallengeCredential"
	return contract.DefineJSONValue[MFAChallengeCredential](contract.Schema{Root: id, Types: []contract.Type{{ID: id, Kind: contract.StringKind}}})
}
