package auth

import "github.com/weiloon1234/Foundry-Go/fault"

// LimitPolicy selects what a credential store does when issuing would exceed a
// subject's active-credential cap. Pending-MFA credentials use their own small
// cap and always replace the oldest pending credential.
type LimitPolicy string

const (
	// RejectNew keeps existing credentials and fails issuance with CredentialLimit.
	RejectNew LimitPolicy = "reject_new"
	// EvictOldest revokes the subject's oldest active credentials of the same
	// kind so the new one fits. It never revokes pending-MFA credentials.
	EvictOldest LimitPolicy = "evict_oldest"
)

func (p LimitPolicy) Validate() error {
	if p != RejectNew && p != EvictOldest {
		return fault.New(fault.Invalid, "invalid credential limit policy")
	}
	return nil
}
