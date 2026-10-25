package contract

import (
	"context"
	jsonv2 "encoding/json/v2"
	"go/token"
	"reflect"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/gotype"
	"github.com/weiloon1234/Foundry-Go/internal/jsoncodec"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/model"
)

// JSON retains the concrete DTO type at a transport boundary. Its zero value is
// invalid. Generated declarations own schema discovery; consumers obtain the
// descriptor from generated code rather than maintaining a second schema.
type JSON[T any] struct {
	_          [0]*T
	schema     *compiledSchema
	err        error
	sourceName string
}

// DefineJSON is the explicit metadata construction boundary used by generated
// declarations. It copies and validates the graph, requiring its root identity
// to match T's qualified Go name. Executable main packages retain the generated
// source namespace because Go reports their runtime package path as "main".
// T must be an exported named struct and must
// not implement model.Identifiable. Registries must Validate before accepting a
// descriptor. Generator analysis owns field/codec agreement and nested-model
// rejection; this function does not infer Go fields or execute custom codecs.
func DefineJSON[T any](description Schema) JSON[T] {
	typ := reflect.TypeFor[T]()
	if typ.Kind() != reflect.Struct || !jsonDeclarationMatches(typ, description.Root) {
		return JSON[T]{err: invalidSchema()}
	}
	schema, err := compileSchema(description)
	if err == nil && (schema.types[description.Root].Kind != ObjectKind || schema.types[description.Root].Nullable) {
		schema, err = nil, invalidSchema()
	}
	return JSON[T]{schema: schema, err: err, sourceName: gotype.Source(string(description.Root))}
}

// jsonDeclarationMatches is shared by ordinary DTO and custom-value boundaries.
// It inspects type capabilities without invoking a model's identity method.
func jsonDeclarationMatches(typ reflect.Type, id TypeID) bool {
	return typ.Name() != "" && typ.PkgPath() != "" && token.IsExported(typ.Name()) &&
		!jsonModelType(typ) && dtoIdentityMatches(typ.PkgPath(), typ.Name(), id)
}

// Model rejection is shared by complete DTO and explicit field boundaries.
// Inspect method sets without calling a model's identity method.
func jsonModelType(typ reflect.Type) bool {
	identity := reflect.TypeFor[model.Identifiable]()
	seen := make(map[reflect.Type]bool)
	for !seen[typ] {
		seen[typ] = true
		if typ.Implements(identity) || reflect.PointerTo(typ).Implements(identity) {
			return true
		}
		if typ.Kind() != reflect.Pointer {
			return false
		}
		typ = typ.Elem()
	}
	return false
}

func dtoIdentityMatches(runtimePackage, name string, id TypeID) bool {
	runtimeName := gotype.Compact(runtimePackage + "." + name)
	id = TypeID(gotype.Compact(string(id)))
	if id == TypeID(runtimeName) {
		return true
	}
	// Executables also erase package paths inside a generic argument, even
	// when the outer value belongs to an imported framework package.
	return mainTypeIdentityMatches(runtimeName, string(id))
}

// IsZero reports an unconstructed descriptor, as opposed to one whose
// declaration failed. Callers use it to substitute an inferred descriptor.
func (d JSON[T]) IsZero() bool { return d.schema == nil && d.err == nil && d.sourceName == "" }

// Validate checks this immutable declaration without invoking DTO methods.
func (d JSON[T]) Validate() error {
	if d.err != nil {
		return d.err
	}
	if d.schema == nil {
		return invalidSchema()
	}
	return nil
}

// Description returns an owned normalized graph for a contract exporter.
func (d JSON[T]) Description() (Schema, error) {
	if err := d.Validate(); err != nil {
		return Schema{}, err
	}
	return d.schema.snapshot(), nil
}

// MaxJSONDepth is the shared parser's maximum child depth (the root is zero).
const MaxJSONDepth = jsonwire.MaxDepth

// JSONLimits bounds one document. Bytes, Nodes, Steps and Issues must be
// positive; Depth is in [0, MaxJSONDepth]. Nodes bounds the wire tree, including
// object names. Steps bounds graph visits, including aliases and missing fields.
// Issues caps returned field diagnostics. Transport adapters own their defaults.
type JSONLimits struct {
	Bytes  int
	Depth  int
	Nodes  int
	Steps  int
	Issues int
}

func (l JSONLimits) Validate() error {
	if l.Bytes <= 0 || l.Nodes <= 0 || l.Steps <= 0 || l.Issues <= 0 || l.Depth < 0 || l.Depth > MaxJSONDepth {
		return safeerror.NewFault(fault.Invalid, "invalid JSON transport limits")
	}
	return nil
}

// DecodeError reports malformed input or a mismatch with the declared wire
// contract. Its message never includes received values or a codec's error text.
// Issues returns owned diagnostics; Unwrap preserves internal cause inspection.
type DecodeError struct {
	issues []Issue
	cause  error
}

func (*DecodeError) Error() string        { return "invalid JSON input" }
func (e *DecodeError) GoString() string   { return e.Error() }
func (*DecodeError) Is(target error) bool { return target == fault.Invalid }
func (e *DecodeError) Unwrap() error      { return e.cause }
func (e *DecodeError) Issues() []Issue    { return slices.Clone(e.issues) }

func decodeFailure(issues []Issue, cause error) error {
	failure := &DecodeError{issues: issues, cause: cause}
	return safeerror.Owned(failure.Error(), failure)
}

// Decode validates exact names, required fields, nullability and declared value
// representations before constructing a fresh T. It rejects duplicate keys and
// lossy Unicode through the shared wire parser. Dynamic JSON numbers remain
// json.Number. Every failure returns zero T, never a partially decoded value.
//
// Callers must bound buffering before supplying data and keep data unchanged
// until this call returns. Custom codecs must be deterministic, concurrency-safe
// and return owned values. Panic and Goexit become internal failures; cancellation
// is checked around bounded parsing and after a codec returns. An uncooperative
// codec is never abandoned while it still owns work or resources.
func (d JSON[T]) Decode(ctx context.Context, data []byte, limits JSONLimits) (T, error) {
	if err := d.Validate(); err != nil {
		return *new(T), err
	}
	if err := d.schema.checkInput(ctx, data, limits); err != nil {
		return *new(T), err
	}
	var result T
	err := callback.Isolated("decode JSON DTO", func() error {
		return decodeNativeJSON(data, &result)
	})
	if err := decodeJSONOutcome(ctx, err); err != nil {
		return *new(T), err
	}
	return result, nil
}

func (s *compiledSchema) checkInput(ctx context.Context, data []byte, limits JSONLimits) error {
	if err := limits.Validate(); err != nil {
		return err
	}
	if ctx == nil {
		return safeerror.NewFault(fault.Invalid, "JSON decode requires a context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	node, err := jsonwire.Decode(data, jsonwire.Limits{Bytes: limits.Bytes, Depth: limits.Depth, Nodes: limits.Nodes})
	if canceled := ctx.Err(); canceled != nil {
		return canceled
	}
	if err != nil {
		return decodeFailure(nil, err)
	}
	issues, err := s.check(ctx, node, shapeLimits{steps: limits.Steps, issues: limits.Issues})
	if err != nil {
		if failure := jsonKeyInternalFailure(err); failure != nil {
			return failure
		}
		if canceled := ctx.Err(); canceled != nil {
			return canceled
		}
		return decodeFailure(issues, err)
	}
	if len(issues) != 0 {
		return decodeFailure(issues, nil)
	}
	return nil
}

func decodeJSONOutcome(ctx context.Context, err error) error {
	// Isolated returns its own panic/Goexit fault directly. Do not invoke an
	// arbitrary codec error's Is/As/Unwrap methods while classifying it. Preserve
	// an actual callback failure even if cancellation arrived during the codec.
	if safeerror.IsPanic(err) {
		return safeerror.WrapFailure(fault.Internal, "JSON DTO codec failed", err)
	}
	if canceled := ctx.Err(); canceled != nil {
		return canceled
	}
	if err != nil {
		return decodeFailure(nil, err)
	}
	return nil
}

func decodeNativeJSON[T any](data []byte, result *T) error {
	return jsoncodec.UnmarshalExact(data, result, jsonv2.RejectUnknownMembers(true))
}
