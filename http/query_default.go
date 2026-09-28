package http

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/value"
)

const maxQueryDefaultBytes = 16 << 10

// DefaultQueryParam uses a declared value only when the query key is omitted.
// Explicit zero, false and empty input still pass through the codec. Duplicate
// keys retain ordinary scalar rejection. Encoding always writes the actual
// field value; it does not silently omit values equal to the default.
//
// Construction snapshots the default's canonical URL spelling through the
// supplied codec and checks its round trip before retaining it. Codec methods
// run inside an owned callback boundary and must finish. Each omitted decode
// parses that immutable spelling afresh, so defaults do not share mutable field
// values across requests. Metadata exposes the same spelling as DefaultURL;
// defaults are public declaration data.
func DefaultQueryParam[Q, V any](name string, codec QueryCodec[V], defaultValue V, field func(*Q) *V) QueryParameter[Q] {
	parameter := QueryParam(name, codec, field)
	parameter.info.Required = false
	if parameter.decode == nil || parameter.encode == nil {
		return parameter
	}
	var text string
	err := callback.Isolated("query default declaration", func() error {
		var err error
		text, err = codec.Format(defaultValue)
		if err != nil {
			return err
		}
		if len(text) > maxQueryDefaultBytes || !utf8.ValidString(text) {
			return fault.New(fault.Invalid, "query default requires bounded UTF-8 metadata")
		}
		decoded, err := codec.Parse(text)
		if err != nil {
			return err
		}
		canonical, err := codec.Format(decoded)
		if err != nil {
			return err
		}
		if canonical != text {
			return fault.New(fault.Invalid, "query default is not a canonical codec value")
		}
		return nil
	})
	if err != nil {
		parameter.err = fault.Wrap(fault.Invalid, "invalid query default declaration", err)
		return parameter
	}
	text = strings.Clone(text)
	parameter.info.DefaultURL = value.Set(text)
	decode := parameter.decode
	parameter.decode = func(ctx context.Context, query *Q, values []string) *queryBindingFailure {
		omitted := len(values) == 0
		if omitted {
			values = []string{text}
		}
		failed := decode(ctx, query, values)
		if omitted && failed != nil && failed.cause != ctx.Err() {
			// The client did not supply this value. A previously validated default
			// failing to decode is a broken server declaration/codec, not user input.
			failed.internal = true
		}
		return failed
	}
	return parameter
}
