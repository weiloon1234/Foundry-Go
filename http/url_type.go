package http

import (
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// URLType supplies the source identity for an already-described scalar codec.
// Generated declarations use this boundary because executable main packages and
// their generic arguments cannot recover source import paths from reflection.
// It preserves V, the codec's syntax, scalar shape and native Parse/Format rules.
// Source IDs are owned by generation; custom authors must supply the actual
// source identity. Opaque codecs require DescribeURL before using this function.
func URLType[V any](id contract.TypeID, codec PathCodec[V]) PathCodec[V] {
	invalid := func(err error) PathCodec[V] { return describedURLCodec[V]{err: err} }
	if !validPathCodec(codec) {
		return invalid(fault.New(fault.Invalid, "URL type requires a described codec"))
	}
	source, ok := codec.(urlScalarDescriber)
	if !ok {
		return invalid(fault.New(fault.Invalid, "URL type requires explicit scalar metadata"))
	}
	info, err := source.urlScalar()
	if err != nil {
		return invalid(err)
	}
	info.Value.ID = id
	return describeURL(codec, contract.DefineScalar[V](info.Value), info.Syntax)
}
