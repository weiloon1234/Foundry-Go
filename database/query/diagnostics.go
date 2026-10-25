package query

import (
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

func databaseFailure(operation string, code database.Code) error {
	failure := database.NewError(operation, code)
	return safeerror.Owned(failure.Error(), failure)
}
