package invalid

import (
	"foundry.test/consumer/multifactor"
	"foundry.test/consumer/recovering"
	"github.com/weiloon1234/Foundry-Go/auth/mfa"
)

func wrong(id mfa.EnrollmentID[recovering.Member]) {
	_ = multifactor.ConfirmationRequest{EnrollmentID: id}
}
