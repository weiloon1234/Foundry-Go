package invalid

import (
	"foundry.test/consumer/multifactor"
	"foundry.test/consumer/recovering"
	"github.com/weiloon1234/Foundry-Go/auth/mfa"
)

func wrong(id mfa.EnrollmentID[recovering.Member]) mfa.EnrollmentID[multifactor.Account] {
	return mfa.EnrollmentID[multifactor.Account](id)
}
