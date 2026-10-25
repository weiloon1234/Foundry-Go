package attachments

import "github.com/weiloon1234/Foundry-Go/internal/safeerror"

// joinFailures retains attachment diagnostics across cleanup aggregation.
func joinFailures(failures ...error) error {
	return safeerror.Join("attachment operation failed", failures...)
}
