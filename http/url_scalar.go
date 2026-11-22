package http

import (
	"reflect"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/enum"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/jsonshape"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
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
	nonEmpty    bool
	err         error
}

func describeURL[V any](codec PathCodec[V], description contract.Scalar[V], syntax URLSyntax) PathCodec[V] {
	wire, err := description.Description()
	if !validPathCodec(codec) {
		err = safeerror.NewFault(fault.Invalid, "URL scalar requires a codec")
	}
	return describedURLCodec[V]{codec: codec, description: description, syntax: syntax, nonEmpty: err == nil && wire.NonEmpty, err: err}
}

// Parse returns any retained descriptor error and enforces a declared NonEmpty
// contract before parsing through the wrapped codec.
func (c describedURLCodec[V]) Parse(text string) (V, error) {
	if c.err != nil {
		return *new(V), c.err
	}
	if c.nonEmpty && text == "" {
		return *new(V), safeerror.NewFault(fault.Invalid, "URL scalar value must not be empty")
	}
	return c.codec.Parse(text)
}

// Format returns any retained descriptor error before formatting through the wrapped
// codec, and enforces a declared NonEmpty contract on the formatted text.
func (c describedURLCodec[V]) Format(value V) (string, error) {
	if c.err != nil {
		return "", c.err
	}
	text, err := c.codec.Format(value)
	if err != nil {
		return "", err
	}
	if c.nonEmpty && text == "" {
		return "", safeerror.NewFault(fault.Invalid, "URL scalar value must not be empty")
	}
	return text, nil
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

// Parse decodes enum text and requires membership in the declared enum.
func (c enumURLCodec[E, P]) Parse(text string) (E, error) {
	value, err := (textPathCodec[E, P]{}).Parse(text)
	if err != nil {
		return *new(E), err
	}
	if !c.descriptor.Contains(value) {
		return *new(E), safeerror.NewFault(fault.Invalid, "invalid enum URL value")
	}
	return value, nil
}

// Format requires a declared enum value before invoking its text marshaler.
func (c enumURLCodec[E, P]) Format(value E) (string, error) {
	if !c.descriptor.Contains(value) {
		return "", safeerror.NewFault(fault.Invalid, "invalid enum URL value")
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
	if format != "" {
		return nativeURLCodec[V](codec, contract.Type{Kind: contract.StringKind, Format: format, NonEmpty: true}, FormattedURLSyntax)
	}
	var zero V
	if provider, ok := any(zero).(interface{ JSONContract() contract.JSON[V] }); ok {
		if description, err := provider.JSONContract().Description(); err == nil && len(description.Types) == 1 && description.Types[0].Kind == contract.StringKind && !description.Types[0].Nullable {
			syntax := TextURLSyntax
			if description.Types[0].Format != "" {
				syntax = FormattedURLSyntax
			}
			return describeURL(codec, contract.DefineScalar[V](description.Types[0]), syntax)
		}
	}
	return codec
}
