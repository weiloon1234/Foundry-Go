package validation

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
)

// Limits bounds one Check. Checks counts rule and field visits; Issues caps
// retained diagnostics. Depth bounds composition and ValueBytes bounds native
// text before scanning it. Callbacks own their internal work and allocations.
type Limits struct {
	Checks     int
	Issues     int
	Depth      int
	ValueBytes int
}

func DefaultLimits() Limits { return Limits{Checks: 65536, Issues: 32, Depth: 32, ValueBytes: 1 << 20} }
func (l Limits) Validate() error {
	if l.Checks <= 0 || l.Issues <= 0 || l.Depth < 0 || l.Depth > jsonwire.MaxDepth || l.ValueBytes <= 0 {
		return invalid("invalid validation limits")
	}
	return nil
}

// LimitError means validation could not complete within its work/value bound.
// It is distinct from rejected rule input and never establishes a valid result.
type LimitError struct{}

func (*LimitError) Error() string        { return "validation resource bound exceeded" }
func (*LimitError) Is(target error) bool { return target == fault.Invalid }
