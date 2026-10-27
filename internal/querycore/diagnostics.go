package querycore

import (
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// DatabaseFailure snapshots known framework operation and code text. Public
// database.NewError remains untrusted; only internal query owners use this helper.
func DatabaseFailure(operation string, code database.Code) error {
	failure := database.NewError(operation, code)
	return safeerror.Owned(failure.Error(), failure)
}
