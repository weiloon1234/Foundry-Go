package callback

import (
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// RetainDiagnostic recovers a source-issued immutable marker hidden by a foreign
// wrapper after the foreign operation finishes. Inspection is bounded and
// contained; it never formats errors or opens opaque application capsules.
// The complete result, including outcome and cleanup causes, is preserved.
func RetainDiagnostic(foreign, result error) error {
	if foreign == nil {
		return result
	}
	var marker error
	inspected := Isolated("inspect foreign diagnostic", func() error {
		errorgraph.Walk(foreign, func(current error) bool {
			if _, ok := safeerror.OwnedMessage(current); ok {
				marker = current
				return false
			}
			return true
		})
		return nil
	})
	if inspected != nil {
		return result
	}
	return safeerror.Retain(marker, result)
}
