package email

import (
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
)

// Kind describes whether a failed submission is safe to retry. Only Transient
// means known non-acceptance with a potentially recoverable cause.
type Kind string

const (
	Construction Kind = "email construction failed"
	Permanent    Kind = "email permanently rejected"
	Transient    Kind = "email temporarily unavailable before acceptance"
	Ambiguous    Kind = "email acceptance is unknown"
)

func (k Kind) Error() string { return string(k) }

// Classification inspects extension errors inside callback isolation. Unknown
// errors, incomplete error graphs, panic and Goexit fail conservatively;
// underlying text is never exposed. Custom error methods must return.
func Classification(err error) Kind {
	if err == nil {
		return ""
	}
	result := Ambiguous
	if failed := callback.Isolated("classify email failure", func() error {
		kinds := [...]Kind{Ambiguous, Permanent, Construction, Transient}
		best := len(kinds)
		complete := errorgraph.Walk(err, func(current error) bool {
			for i, kind := range kinds[:best] {
				if errorgraph.Matches(current, kind) {
					best, result = i, kind
					break
				}
			}
			return best != 0
		})
		if !complete {
			result = Ambiguous
		}
		return nil
	}); failed != nil {
		return Ambiguous
	}
	return result
}
