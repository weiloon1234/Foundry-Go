package foundation

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

func registrationFailure(code fault.Code, message string) error {
	return safeerror.NewFault(code, message)
}
