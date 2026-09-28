package publisher

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
)

// Failure inspection must not strand a publication transaction or consume its
// row as permanent without a known reason. Unknown, cyclic or faulty error
// graphs remain retryable under the publisher's configured attempt limit.
func permanentFailure(err error) bool {
	if err == nil {
		return false
	}
	permanent := false
	inspect := callback.Isolated("classify outbox publication failure", func() error {
		complete := errorgraph.Walk(err, func(current error) bool {
			permanent = errorgraph.Matches(current, fault.Invalid) || errorgraph.Matches(current, fault.Missing)
			return !permanent
		})
		permanent = permanent && complete
		return nil
	})
	return inspect == nil && permanent
}
