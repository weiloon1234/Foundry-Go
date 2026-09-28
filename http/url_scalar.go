package http

import (
	"reflect"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/enum"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/jsonshape"
)

// URLSyntax describes the scalar codec's lexical contract before URL escaping.
// Cardinality and path structure remain in the parameter's own descriptor.
type URLSyntax string

const (
	TextURLSyntax      URLSyntax = "text"
	IntegerURLSyntax   URLSyntax = "integer"
	FloatURLSyntax     URLSyntax = "float"
	BooleanURLSyntax   URLSyntax = "boolean"
	ModelIDURLSyntax   URLSyntax = "model_id"
	EnumURLSyntax      URLSyntax = "enum"
	FormattedURLSyntax URLSyntax = "formatted"
	CustomURLSyntax    URLSyntax = "custom"
)

// URLScalarInfo describes the decoded scalar value and the URL codec used by
// the runtime. Custom syntax retains server-owned parsing rules; its declared
// value shape does not imply an equivalent browser implementation.
type URLScalarInfo struct {
	Value  contract.Type `json:"value"`
	Syntax URLSyntax     `json:"syntax"`
}

// DescribeURL attaches an explicit same-value scalar contract to a custom path,
// query or cookie codec. Custom Parse/Format remain authoritative. An invalid
// descriptor or nil codec rejects binding; missing metadata is never guessed.
func DescribeURL[V any](codec PathCodec[V], description contract.Scalar[V]) PathCodec[V] {
	return describeURL(codec, description, CustomURLSyntax)
}

type describedURLCodec[V any] struct {
	codec       PathCodec[V]
	description contract.Scalar[V]
	syntax      URLSyntax
	err         error
}

func describeURL[V any](codec PathCodec[V], description contract.Scalar[V], syntax URLSyntax) PathCodec[V] {
	err := description.Validate()
	if !validPathCodec(codec) {
		err = fault.New(fault.Invalid, "URL scalar requires a codec")
	}
	return describedURLCodec[V]{codec: codec, description: description, syntax: syntax, err: err}
}

func (c describedURLCodec[V]) Parse(text string) (V, error) {
	if c.err != nil {
		return *new(V), c.err
	}
	return c.codec.Parse(text)
}
func (c describedURLCodec[V]) Format(value V) (string, error) {
	if c.err != nil {
		return "", c.err
	}
	return c.codec.Format(value)
}
func (c describedURLCodec[V]) urlScalar() (URLScalarInfo, error) {
	if c.err != nil {
		return URLScalarInfo{}, c.err
	}
	description, err := c.description.Description()
	return URLScalarInfo{Value: description, Syntax: c.syntax}, err
}

// The private capability is available only from framework codec constructors.
// Third-party codecs use DescribeURL, retaining the concrete V at that boundary.
type urlScalarDescriber interface{ urlScalar() (URLScalarInfo, error) }

func nativeURLCodec[V any](codec PathCodec[V], wire contract.Type, syntax URLSyntax) PathCodec[V] {
	typ := reflect.TypeFor[V]()
	wire.ID = contract.TypeID(typ.String())
	if typ.Name() != "" && typ.PkgPath() != "" {
		wire.ID = contract.TypeID(typ.PkgPath() + "." + typ.Name())
	}
	return describeURL(codec, contract.DefineScalar[V](wire), syntax)
}

func validPathCodec[V any](codec PathCodec[V]) bool {
	if codec == nil {
		return false
	}
	value := reflect.ValueOf(codec)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return !value.IsNil()
	}
	return true
}

// EnumPath reuses the concrete enum descriptor for client metadata and its text
// methods for execution. Generation supplies the same enum's own descriptor.
func EnumPath[E enum.Scalar, P PathTextPointer[E]](descriptor enum.Descriptor[E]) PathCodec[E] {
	id := contract.TypeID(descriptor.PackagePath() + "." + descriptor.Name())
	return describeURL(enumURLCodec[E, P]{descriptor: descriptor}, contract.DefineScalar[E](contract.EnumType(id, descriptor)), EnumURLSyntax)
}

type enumURLCodec[E enum.Scalar, P PathTextPointer[E]] struct{ descriptor enum.Descriptor[E] }

func (c enumURLCodec[E, P]) Parse(text string) (E, error) {
	value, err := (textPathCodec[E, P]{}).Parse(text)
	if err != nil {
		return *new(E), err
	}
	if !c.descriptor.Contains(value) {
		return *new(E), fault.New(fault.Invalid, "invalid enum URL value")
	}
	return value, nil
}
func (c enumURLCodec[E, P]) Format(value E) (string, error) {
	if !c.descriptor.Contains(value) {
		return "", fault.New(fault.Invalid, "invalid enum URL value")
	}
	return (textPathCodec[E, P]{}).Format(value)
}

// EnumQuery shares EnumPath's typed membership metadata and text conversion.
func EnumQuery[E enum.Scalar, P QueryTextPointer[E]](descriptor enum.Descriptor[E]) QueryCodec[E] {
	return EnumPath[E, P](descriptor)
}

func knownTextURLCodec[V any, P PathTextPointer[V]]() PathCodec[V] {
	codec := textPathCodec[V, P]{}
	typ := reflect.TypeFor[V]()
	format := contract.Format(jsonshape.ScalarFormat(typ.PkgPath(), typ.Name()))
	if format == "" {
		return codec
	}
	return nativeURLCodec[V](codec, contract.Type{Kind: contract.StringKind, Format: format}, FormattedURLSyntax)
}
