package attachments

import (
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/storage"
)

// Only the direct storage result establishes this publication's outcome.
// Nested causes may describe cleanup or another publication.
func storageUnchanged(cause error) bool {
	detail, ok := safeerror.UnwrapOwned(cause).(*storage.Error)
	return ok && detail != nil && detail.Outcome() == storage.Unchanged
}
