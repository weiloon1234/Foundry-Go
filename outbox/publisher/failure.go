package publisher

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
)

// Failure inspection must not strand a publication transaction or consume its
// row as permanent without a known reason. Only fault.Invalid (a message that
// can never be accepted) is permanent. fault.Missing stays transient: during a
// rolling deploy another replica may already declare the job or event, and a
// later attempt of this row can succeed. Unknown, cyclic or faulty error graphs
// remain retryable under the publisher's configured attempt/age budget.
func permanentFailure(err error) bool {
	if err == nil {
		return false
	}
	permanent := false
	inspect := callback.Isolated("classify outbox publication failure", func() error {
		complete := errorgraph.Walk(err, func(current error) bool {
			permanent = errorgraph.Matches(current, fault.Invalid)
			return !permanent
		})
		permanent = permanent && complete
		return nil
	})
	return inspect == nil && permanent
}
