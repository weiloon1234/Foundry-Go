package http

import (
	"context"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/httpheader"
	"github.com/weiloon1234/Foundry-Go/internal/httpquery"
	"github.com/weiloon1234/Foundry-Go/internal/jsonpointer"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// Query declares query and request header bindings on one concrete value without
// an untyped map at the consumer boundary. DefineQuery snapshots its declarations;
// the zero descriptor is invalid. Generated declarations use these same bindings.
type Query[Q any] struct {
	parameters    []QueryParameter[Q]
	indexes       map[string]int
	headerIndexes map[string]int
	headerFields  []requestHeaderField
	err           error
}

// DefineQuery declares case-sensitive query names and case-insensitive header
// names in separate namespaces. Brackets and dots in query names are literal
// characters, not implicit nesting. Declaration order does not
// affect encoding or field diagnostics. Call Validate during route registration.
func DefineQuery[Q any](parameters ...QueryParameter[Q]) Query[Q] {
	d := Query[Q]{parameters: slices.Clone(parameters), indexes: make(map[string]int), headerIndexes: make(map[string]int)}
	slices.SortFunc(d.parameters, func(a, b QueryParameter[Q]) int {
		if a.source != b.source {
			return int(a.source) - int(b.source)
		}
		return strings.Compare(a.info.Name, b.info.Name)
	})
	for i, parameter := range d.parameters {
		if parameter.err != nil {
			d.err = parameter.err
			break
		}
		indexes, name := d.indexes, parameter.info.Name
		valid := httpquery.ValidName(name)
		switch parameter.source {
		case queryParameterSource:
		case headerParameterSource:
			valid = httpheader.ValidateRequestName(name) == nil && !parameter.info.Repeated && !parameter.info.DefaultURL.IsSet()
			indexes, name = d.headerIndexes, strings.ToLower(name)
		default:
			valid = false
		}
		_, duplicate := indexes[name]
		if !valid || duplicate || parameter.decode == nil || parameter.encode == nil {
			d.err = safeerror.NewFault(fault.Invalid, "query parameter requires a unique name, codec and field selector")
			break
		}
		indexes[name] = i
		if parameter.source == headerParameterSource {
			d.headerFields = append(d.headerFields, requestHeaderField{name: parameter.info.Name, required: parameter.info.Required, index: i})
		}
	}
	return d
}

// ValidateQueryDescriptor checks immutable binding declarations without calling user methods.
func ValidateQueryDescriptor[Q any](d Query[Q]) error {
	if d.err != nil {
		return d.err
	}
	if d.indexes == nil || d.headerIndexes == nil {
		return safeerror.NewFault(fault.Invalid, "query descriptor is not defined")
	}
	return nil
}

// QueryParameters returns owned, sorted cardinality and scalar metadata for
// query bindings only. Use RequestHeaderParameters to inspect header bindings.
func QueryParameters[Q any](d Query[Q]) ([]QueryParameterInfo, error) {
	return queryParameterMetadata(d, queryParameterSource)
}

func queryParameterMetadata[Q any](d Query[Q], source parameterSource) ([]QueryParameterInfo, error) {
	if err := ValidateQueryDescriptor(d); err != nil {
		return nil, err
	}
	parameters := make([]QueryParameterInfo, 0)
	for _, parameter := range d.parameters {
		if parameter.source != source {
			continue
		}
		info := parameter.info
		if parameter.scalar != nil {
			scalar, err := parameter.scalar()
			if err != nil {
				return nil, err
			}
			info.Scalar = &scalar
		}
		parameters = append(parameters, info)
	}
	return parameters, nil
}

func validateBodyQuery[Q any](d Query[Q]) error {
	if err := ValidateQueryDescriptor(d); err != nil {
		return err
	}
	if len(d.headerIndexes) != 0 {
		return safeerror.NewFault(fault.Invalid, "body fields cannot declare request header bindings")
	}
	return nil
}

// QueryLimits bounds encoded bytes, parameter slots and returned diagnostics.
// All fields must be positive. HTTP endpoint adapters own their default limits.
type QueryLimits struct{ Bytes, Pairs, Issues int }

func (l QueryLimits) wire() httpquery.Limits { return httpquery.Limits{Bytes: l.Bytes, Pairs: l.Pairs} }

// Validate checks wire decoding bounds and requires a positive diagnostic issue limit.
func (l QueryLimits) Validate() error {
	if err := l.wire().Validate(); err != nil {
		return err
	}
	if l.Issues <= 0 {
		return safeerror.NewFault(fault.Invalid, "invalid query diagnostic limit")
	}
	return nil
}

// QueryError reports malformed transport input, an invalid field representation
// or invalid query output. Issues use JSON Pointer syntax over declared query
// names; unknown names report at the root and are never echoed. Error text never
// includes submitted names/values or custom codec messages. Causes are retained
// for deliberate internal inspection. Every failed Decode/Encode returns zero.
type QueryError struct {
	issues []contract.Issue
	cause  error
}

// Error returns a generic query failure message without exposing input.
func (*QueryError) Error() string { return "invalid query parameters" }

// GoString returns the generic query failure message.
func (e *QueryError) GoString() string { return e.Error() }

// Is reports whether target is fault.Invalid.
func (*QueryError) Is(target error) bool { return target == fault.Invalid }

// Unwrap returns the underlying cause.
func (e *QueryError) Unwrap() error { return e.cause }

// Issues returns a copy of the query validation issues.
func (e *QueryError) Issues() []contract.Issue { return slices.Clone(e.issues) }

func queryFailure(issues []contract.Issue, cause error) error {
	failure := &QueryError{issues: issues, cause: cause}
	return safeerror.Owned(failure.Error(), failure)
}

func queryPrepare[Q any](d Query[Q], ctx context.Context, limits QueryLimits) error {
	if err := ValidateQueryDescriptor(d); err != nil {
		return err
	}
	if err := limits.Validate(); err != nil {
		return err
	}
	if ctx == nil {
		return safeerror.NewFault(fault.Invalid, "query binding requires a context")
	}
	return ctx.Err()
}

// DecodeQuery rejects unknown names, duplicate scalar values, missing required fields
// and malformed wire input before invoking any codec. Repeated values retain
// their order. No body or path values are merged into the query. Header bindings
// are excluded: this function returns only the URL query projection of Q.
//
// Codec and field-selector execution is owned until it returns, even after
// cancellation. It runs on the caller's goroutine: panics become internal
// failures, and runtime.Goexit ends the goroutine as any Go call does. Custom codecs must
// be deterministic and concurrency-safe and must not retain input references
// that they can mutate. Every failure discards the partially constructed Q.
func DecodeQuery[Q any](d Query[Q], ctx context.Context, raw string, limits QueryLimits) (Q, error) {
	if err := queryPrepare(d, ctx, limits); err != nil {
		return *new(Q), err
	}
	values, err := httpquery.Parse(ctx, raw, limits.wire())
	if err != nil {
		if canceled := ctx.Err(); canceled != nil {
			return *new(Q), canceled
		}
		return *new(Q), queryFailure([]contract.Issue{{Code: contract.ValueIssue}}, err)
	}
	return queryDecodeValues(d, ctx, values, limits.Issues)
}

// decodeValues hydrates already bounded, decoded scalar values. URL and multipart
// framing both reuse these cardinality, codec, selector and failure rules.
func queryDecodeValues[Q any](d Query[Q], ctx context.Context, values url.Values, issueLimit int) (Q, error) {
	var issues []contract.Issue
	add := func(path string, code contract.IssueCode) {
		if len(issues) < issueLimit {
			issues = append(issues, contract.Issue{Path: path, Code: code})
		}
	}
	for name := range values {
		if _, known := d.indexes[name]; !known {
			add("", contract.UnknownIssue)
			break
		}
	}
	for _, parameter := range d.parameters {
		if parameter.source != queryParameterSource {
			continue
		}
		count := len(values[parameter.info.Name])
		if code := parameterCardinalityIssue(parameter.info, count); code != "" {
			add(queryIssuePath(parameter.info.Name), code)
		}
	}
	if err := ctx.Err(); err != nil {
		return *new(Q), err
	}
	if len(issues) != 0 {
		return *new(Q), queryFailure(issues, nil)
	}
	var result Q
	var failed *queryBindingFailure
	var info QueryParameterInfo
	err := callback.Invoke("decode query parameters", func() error {
		for _, parameter := range d.parameters {
			if parameter.source != queryParameterSource {
				continue
			}
			if ctx.Err() != nil {
				break
			}
			info = parameter.info
			failed = parameter.decode(ctx, &result, values[info.Name])
			if failed != nil {
				break
			}
		}
		return nil
	})
	if err = queryCallbackResult(ctx, err, failed, info); err != nil {
		return *new(Q), err
	}
	return result, nil
}

// EncodeQuery produces a relative query without a question mark. Nil and empty
// slices are omitted; Optional zero values are omitted. Parameter and value
// types remain concrete at the call site. Query escaping is applied exactly
// once by the shared wire layer. The caller must keep input unchanged until
// return; custom formatters own their internal work and allocation. Header
// bindings are never encoded into the query or required for URL generation.
func EncodeQuery[Q any](d Query[Q], ctx context.Context, input Q, limits QueryLimits) (string, error) {
	if err := queryPrepare(d, ctx, limits); err != nil {
		return "", err
	}
	values := make(url.Values)
	budget := queryBudget{pairs: limits.Pairs, bytes: limits.Bytes}
	var failed *queryBindingFailure
	var info QueryParameterInfo
	err := callback.Invoke("encode query parameters", func() error {
		for _, parameter := range d.parameters {
			if parameter.source != queryParameterSource {
				continue
			}
			if ctx.Err() != nil {
				break
			}
			info = parameter.info
			var items []string
			items, failed = parameter.encode(ctx, &input, &budget)
			if failed != nil {
				break
			}
			if len(items) != 0 {
				values[info.Name] = items
			}
		}
		return nil
	})
	if err = queryCallbackResult(ctx, err, failed, info); err != nil {
		return "", err
	}
	encoded, err := httpquery.Encode(ctx, values, limits.wire())
	if err != nil {
		if canceled := ctx.Err(); canceled != nil {
			return "", canceled
		}
		return "", queryFailure(nil, err)
	}
	return encoded, nil
}

func queryCallbackResult(ctx context.Context, callbackErr error, failed *queryBindingFailure, info QueryParameterInfo) error {
	// No arbitrary error methods are invoked to classify codec failures.
	if callbackErr != nil {
		return safeerror.WrapFailure(fault.Internal, "query binding callback failed", callbackErr)
	}
	if failed != nil && failed.internal {
		return safeerror.WrapFailure(fault.Internal, "query field binding failed", failed.cause)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if failed == nil {
		return nil
	}
	path := queryIssuePath(info.Name)
	if info.Repeated {
		path += "/" + strconv.Itoa(failed.index)
	}
	return queryFailure([]contract.Issue{{Path: path, Code: contract.ValueIssue}}, failed.cause)
}

func queryIssuePath(name string) string {
	return jsonpointer.Append("", name)
}

// Parameter cardinality is independent of URL or multipart framing.
func parameterCardinalityIssue(info QueryParameterInfo, count int) contract.IssueCode {
	if count == 0 && info.Required {
		return contract.RequiredIssue
	}
	if count > 1 && !info.Repeated {
		return contract.LengthIssue
	}
	return ""
}
