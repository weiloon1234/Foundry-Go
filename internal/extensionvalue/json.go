// Package extensionvalue shares the bounded canonical value boundary used by
// metadata and settings. The public contract descriptor is the schema source.
package extensionvalue

import (
	"context"
	"encoding/json"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

const MaxBytes = 256 << 10
const MaxBatchBytes = 4 << 20

// ErrBatchLimit reports a batch retaining more than MaxBatchBytes of JSON. It
// matches fault.Conflict; slot loading retries such a batch in smaller parts.
var ErrBatchLimit = fault.New(fault.Conflict, "model extension JSON batch exceeds its byte limit")

// BatchBudget bounds retained JSON across a streamed result. A caller returns
// no partial result when this fails; one individually bounded row is decoded at
// a time by the ordinary query API.
type BatchBudget struct{ bytes int }

func (b *BatchBudget) Add(snapshot value.JSON[json.RawMessage]) error {
	text, err := snapshot.Text()
	if err != nil {
		return err
	}
	if len(text) > MaxBatchBytes-b.bytes {
		return ErrBatchLimit
	}
	b.bytes += len(text)
	return nil
}

func Limits() contract.JSONLimits {
	return contract.JSONLimits{Bytes: MaxBytes, Depth: 64, Nodes: 10000, Steps: 100000, Issues: 16}
}
func Encode[V any](ctx context.Context, schema contract.JSON[V], input V) (value.JSON[json.RawMessage], error) {
	data, err := schema.Encode(ctx, input, Limits())
	if err != nil {
		return value.JSON[json.RawMessage]{}, err
	}
	return value.ParseJSON[json.RawMessage](string(data))
}
func Decode[V any](ctx context.Context, schema contract.JSON[V], snapshot value.JSON[json.RawMessage]) (V, error) {
	text, err := snapshot.Text()
	if err != nil {
		return *new(V), err
	}
	return schema.Decode(ctx, []byte(text), Limits())
}
