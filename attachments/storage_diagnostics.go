package attachments

import (
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/storage"
)

func storageFailure(code storage.Code, operation storage.Operation, outcome storage.Outcome, cause error) error {
	failure := storage.Failure(code, operation, outcome, cause)
	return safeerror.Owned(failure.Error(), failure)
}
