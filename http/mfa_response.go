package http

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth/mfa"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/authtransport"
	"github.com/weiloon1234/Foundry-Go/model"
)

// MFAEnrollmentResponse deliberately discloses a committed enrollment's secret
// and provisioning URI. Use after Factors.Enroll with current password verification.
// It requires secure POST and disables caching before the handler runs. Ordinary
// JSON of Enrollment never discloses these values. Failed HTTP delivery cannot
// undo enrollment; repeating Enroll replaces the unconfirmed generation.
func MFAEnrollmentResponse[M any](status int, source clock.Clock) Response[mfa.Enrollment[M]] {
	return credentialResponse(status, authtransport.MFAEnrollmentResponseJSON(), func() error { return credentialClockValid(source) },
		func(_ context.Context, enrollment mfa.Enrollment[M]) (authtransport.MFAEnrollmentResponse, error) {
			var empty authtransport.MFAEnrollmentResponse
			now, err := credentialTime(source)
			if err != nil {
				return empty, err
			}
			if enrollment.ID().IsZero() || enrollment.Secret().Validate() != nil || enrollment.URI().IsZero() || now.UTC().Before(enrollment.CreatedAt().UTC()) || !now.UTC().Before(enrollment.ExpiresAt().UTC()) {
				return empty, fault.New(fault.Invalid, "MFA enrollment is missing or expired")
			}
			id, err := model.ParseID[mfa.Record](enrollment.ID().String())
			if err != nil {
				return empty, err
			}
			return authtransport.MFAEnrollmentResponse{EnrollmentID: id, Secret: enrollment.Secret().Secret().Reveal(),
				ProvisioningURI: enrollment.URI().Reveal(), ExpiresAt: enrollment.ExpiresAt()}, nil
		})
}

// MFARecoveryResponse explicitly delivers the recovery set returned by Confirm
// or RegenerateRecovery through secure POST/no-store. It exposes no model, proof
// or credential. A lost response requires a new factor-authorized regeneration;
// the framework never stores recoverable plaintext codes for later retrieval.
func MFARecoveryResponse[M any](status int) Response[mfa.RecoveryCodes[M]] {
	return credentialResponse(status, authtransport.MFARecoveryResponseJSON(), nil,
		func(_ context.Context, result mfa.RecoveryCodes[M]) (authtransport.MFARecoveryResponse, error) {
			var empty authtransport.MFARecoveryResponse
			codes := result.Codes()
			if result.ID().IsZero() || len(codes) == 0 || len(codes) > mfa.MaxRecoveryCodes {
				return empty, fault.New(fault.Invalid, "MFA recovery set is missing")
			}
			raw := make([]string, len(codes))
			for i, code := range codes {
				if err := code.Validate(); err != nil {
					return empty, err
				}
				raw[i] = code.Secret().Reveal()
			}
			return authtransport.MFARecoveryResponse{RecoveryCodes: raw}, nil
		})
}
