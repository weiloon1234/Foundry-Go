package value

import (
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
)

// JSONEncodingLimits bounds an encoded document and the native input inspection.
// Depth and Nodes describe JSON, with the root at depth zero. Steps separately
// bounds native values, indirections and declared fields visited before encoding.
// Bytes also bounds inspected native string/byte content, before codec execution.
// All limits except Depth must be positive. Depth may not exceed JSONMaxDepth.
// Custom codecs own their internal work and allocation; their emitted document
// still passes the output bounds. Callers must keep input unchanged until return.
type JSONEncodingLimits struct {
	Bytes int
	Depth int
	Nodes int
	Steps int
}

func (l JSONEncodingLimits) Validate() error {
	if l.Bytes <= 0 || l.Depth < 0 || l.Depth > JSONMaxDepth || l.Nodes <= 0 || l.Steps <= 0 {
		return fault.New(fault.Invalid, "invalid JSON encoding limits")
	}
	return nil
}

// EncodeJSON encodes an owned, bounded JSON document with ordinary Go field/tag
// semantics. It preserves transport string NULs and exact json.Number values.
// Optional/Nullable values share the same input inspection as typed snapshots.
// It returns no bytes on failure. It does not infer a DTO schema; contract.JSON
// adds the generated schema checks before a transport can publish these bytes.
// Panic and Goexit in codecs become internal faults. Cancellation never abandons
// a codec that is still running or retaining resources.
func EncodeJSON[T any](ctx context.Context, input T, limits JSONEncodingLimits) ([]byte, error) {
	if ctx == nil {
		return nil, fault.New(fault.Invalid, "JSON encoding requires a context")
	}
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	output := jsonOutput{ctx: ctx, limit: limits.Bytes}
	err := callback.Isolated("encode JSON value", func() error {
		budget := jsonInputBudget{ctx: ctx, limits: &limits, allowNUL: true}
		if err := budget.check(reflect.ValueOf(input), 0); err != nil {
			return err
		}
		return jsonv2.MarshalWrite(&output, input, json.DefaultOptionsV1(),
			jsontext.AllowInvalidUTF8(false), jsontext.AllowDuplicateNames(false))
	})
	if failure, ok := err.(*fault.Error); ok && failure.Code() == fault.Panicked {
		return nil, fault.Wrap(fault.Internal, "JSON value codec failed", err)
	}
	if canceled := ctx.Err(); canceled != nil {
		return nil, canceled
	}
	if err == nil {
		err = output.err
	}
	if err != nil {
		return nil, fault.Wrap(fault.Invalid, "invalid JSON value encoding", err)
	}
	_, err = jsonwire.Decode(output.data, jsonwire.Limits{Bytes: limits.Bytes, Depth: limits.Depth, Nodes: limits.Nodes})
	if canceled := ctx.Err(); canceled != nil {
		return nil, canceled
	}
	if err != nil {
		return nil, fault.Wrap(fault.Invalid, "invalid JSON value encoding", err)
	}
	return output.data, nil
}

type jsonOutput struct {
	ctx   context.Context
	limit int
	data  []byte
	err   error
}

func (w *jsonOutput) Write(data []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	if err := w.ctx.Err(); err != nil {
		w.err = err
		return 0, err
	}
	if len(data) > w.limit-len(w.data) {
		w.err = fault.New(fault.Invalid, "JSON output byte bound exceeded")
		return 0, w.err
	}
	w.data = append(w.data, data...)
	return len(data), nil
}
