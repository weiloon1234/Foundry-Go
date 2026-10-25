package cli

import "github.com/weiloon1234/Foundry-Go/internal/safeerror"

func ownedUsage(message string) error {
	return safeerror.Owned(message, Usage(message))
}
