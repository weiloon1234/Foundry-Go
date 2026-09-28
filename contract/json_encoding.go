package contract

import (
	"context"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
	"github.com/weiloon1234/Foundry-Go/value"
)

// EncodeError reports a value that could not satisfy its response contract.
// Its safe message contains no output fields or codec details. Issues returns
// an owned slice. Unwrap preserves internal cause inspection.
type EncodeError struct {
	issues []Issue
	cause  error
}

func (*EncodeError) Error() string        { return "invalid JSON output" }
func (e *EncodeError) GoString() string   { return e.Error() }
func (*EncodeError) Is(target error) bool { return target == fault.Internal }
func (e *EncodeError) Unwrap() error      { return e.cause }
func (e *EncodeError) Issues() []Issue    { return slices.Clone(e.issues) }

// Encode prepares an owned response body and checks the same schema used by
// Decode and Description. No bytes are returned until encoding and every schema
// check succeed. Transport adapters can therefore set success headers only after
// this operation finishes. Keep input unchanged until return. Native inspection
// and schema validation each receive the declared Steps budget; custom codecs
// own their internal work and must return bounded, owned representations.
func (d JSON[T]) Encode(ctx context.Context, input T, limits JSONLimits) ([]byte, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, fault.New(fault.Invalid, "JSON encoding requires a context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := value.EncodeJSON(ctx, input, value.JSONEncodingLimits{
		Bytes: limits.Bytes, Depth: limits.Depth, Nodes: limits.Nodes, Steps: limits.Steps,
	})
	if err != nil {
		return nil, &EncodeError{cause: err}
	}
	node, err := jsonwire.Decode(data, jsonwire.Limits{Bytes: limits.Bytes, Depth: limits.Depth, Nodes: limits.Nodes})
	if err != nil {
		return nil, &EncodeError{cause: err}
	}
	issues, err := d.schema.check(ctx, node, shapeLimits{steps: limits.Steps, issues: limits.Issues})
	if err != nil {
		return nil, &EncodeError{issues: issues, cause: err}
	}
	if len(issues) != 0 {
		return nil, &EncodeError{issues: issues}
	}
	return data, nil
}
