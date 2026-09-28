package http

import (
	"github.com/weiloon1234/Foundry-Go/fault"
)

// PathParameter binds a named URL parameter to a concrete field of P. Its
// callbacks are assembled by Param; there is no string-based field lookup.
type PathParameter[P any] struct {
	name   string
	scalar func() (URLScalarInfo, error)
	decode func(*P, string) error
	encode func(*P) (string, error)
}

// Param defines a field once for both decoding and named URL generation. The
// field selector must return the address of the selected field on its argument,
// be deterministic and be safe for concurrent calls with independent arguments.
// Generated path declarations can use this same public constructor.
func Param[P, V any](name string, codec PathCodec[V], field func(*P) *V) PathParameter[P] {
	if !validPathCodec(codec) || field == nil {
		return PathParameter[P]{name: name}
	}
	var scalar func() (URLScalarInfo, error)
	if described, ok := codec.(urlScalarDescriber); ok {
		if _, err := described.urlScalar(); err != nil {
			return PathParameter[P]{name: name}
		}
		scalar = described.urlScalar
	}
	return PathParameter[P]{name: name, scalar: scalar, decode: func(path *P, text string) error {
		destination := field(path)
		if destination == nil {
			return fault.New(fault.Internal, "path field selector returned nil")
		}
		decoded, err := codec.Parse(text)
		if err != nil {
			return err
		}
		*destination = decoded
		return nil
	}, encode: func(path *P) (string, error) {
		source := field(path)
		if source == nil {
			return "", fault.New(fault.Internal, "path field selector returned nil")
		}
		return codec.Format(*source)
	}}
}
