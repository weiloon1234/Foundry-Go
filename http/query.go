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
	"github.com/weiloon1234/Foundry-Go/internal/httpquery"
	"github.com/weiloon1234/Foundry-Go/internal/jsonpointer"
)

// Query binds a query string to a concrete value without an untyped parameter
// map at the consumer boundary. DefineQuery snapshots its field declarations;
// the zero descriptor is invalid. Generated declarations use these same bindings.
type Query[Q any] struct {
	parameters []QueryParameter[Q]
	indexes    map[string]int
	err        error
}

// DefineQuery declares exact, case-sensitive names. Brackets and dots in a name
// are literal characters, not implicit nesting. Declaration order does not
// affect encoding or field diagnostics. Call Validate during route registration.
func DefineQuery[Q any](parameters ...QueryParameter[Q]) Query[Q] {
	d := Query[Q]{parameters: slices.Clone(parameters), indexes: make(map[string]int, len(parameters))}
	slices.SortFunc(d.parameters, func(a, b QueryParameter[Q]) int { return strings.Compare(a.info.Name, b.info.Name) })
	for i, parameter := range d.parameters {
		if parameter.err != nil {
			d.err = parameter.err
			break
		}
		_, duplicate := d.indexes[parameter.info.Name]
		if !httpquery.ValidName(parameter.info.Name) || duplicate || parameter.decode == nil || parameter.encode == nil {
			d.err = fault.New(fault.Invalid, "query parameter requires a unique name, codec and field selector")
			break
		}
		d.indexes[parameter.info.Name] = i
	}
	return d
}

// Validate checks immutable binding declarations without calling user methods.
func (d Query[Q]) Validate() error {
	if d.err != nil {
		return d.err
	}
	if d.indexes == nil {
		return fault.New(fault.Invalid, "query descriptor is not defined")
	}
	return nil
}

// Parameters returns an owned, deterministic snapshot of cardinality and scalar metadata.
func (d Query[Q]) Parameters() ([]QueryParameterInfo, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	parameters := make([]QueryParameterInfo, len(d.parameters))
	for i, parameter := range d.parameters {
		parameters[i] = parameter.info
		if parameter.scalar != nil {
			scalar, err := parameter.scalar()
			if err != nil {
				return nil, err
			}
			parameters[i].Scalar = &scalar
		}
	}
	return parameters, nil
}

// QueryLimits bounds encoded bytes, parameter slots and returned diagnostics.
// All fields must be positive. HTTP endpoint adapters own their default limits.
type QueryLimits struct{ Bytes, Pairs, Issues int }

func (l QueryLimits) wire() httpquery.Limits { return httpquery.Limits{Bytes: l.Bytes, Pairs: l.Pairs} }

func (l QueryLimits) Validate() error {
	if err := l.wire().Validate(); err != nil {
		return err
	}
	if l.Issues <= 0 {
		return fault.New(fault.Invalid, "invalid query diagnostic limit")
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

func (*QueryError) Error() string              { return "invalid query parameters" }
func (e *QueryError) GoString() string         { return e.Error() }
func (*QueryError) Is(target error) bool       { return target == fault.Invalid }
func (e *QueryError) Unwrap() error            { return e.cause }
func (e *QueryError) Issues() []contract.Issue { return slices.Clone(e.issues) }

func (d Query[Q]) prepare(ctx context.Context, limits QueryLimits) error {
	if err := d.Validate(); err != nil {
		return err
	}
	if err := limits.Validate(); err != nil {
		return err
	}
	if ctx == nil {
		return fault.New(fault.Invalid, "query binding requires a context")
	}
	return ctx.Err()
}

// Decode rejects unknown names, duplicate scalar values, missing required fields
// and malformed wire input before invoking any codec. Repeated values retain
// their order. No body or path values are merged into the query.
//
// Codec and field-selector execution is owned until it returns, even after
// cancellation. Panics and Goexit become internal failures. Custom codecs must
// be deterministic and concurrency-safe and must not retain input references
// that they can mutate. Every failure discards the partially constructed Q.
func (d Query[Q]) Decode(ctx context.Context, raw string, limits QueryLimits) (Q, error) {
	if err := d.prepare(ctx, limits); err != nil {
		return *new(Q), err
	}
	values, err := httpquery.Parse(ctx, raw, limits.wire())
	if err != nil {
		if canceled := ctx.Err(); canceled != nil {
			return *new(Q), canceled
		}
		return *new(Q), &QueryError{cause: err, issues: []contract.Issue{{Code: contract.ValueIssue}}}
	}
	return d.decodeValues(ctx, values, limits.Issues)
}

// decodeValues hydrates already bounded, decoded scalar values. URL and multipart
// framing both reuse these cardinality, codec, selector and failure rules.
func (d Query[Q]) decodeValues(ctx context.Context, values url.Values, issueLimit int) (Q, error) {
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
		count := len(values[parameter.info.Name])
		if code := parameterCardinalityIssue(parameter.info, count); code != "" {
			add(queryIssuePath(parameter.info.Name), code)
		}
	}
	if err := ctx.Err(); err != nil {
		return *new(Q), err
	}
	if len(issues) != 0 {
		return *new(Q), &QueryError{issues: issues}
	}
	var result Q
	var failed *queryBindingFailure
	var info QueryParameterInfo
	err := callback.Isolated("decode query parameters", func() error {
		for _, parameter := range d.parameters {
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

// Encode produces a relative query without a question mark. Nil and empty
// slices are omitted; Optional zero values are omitted. Parameter and value
// types remain concrete at the call site. Query escaping is applied exactly
// once by the shared wire layer. The caller must keep input unchanged until
// return; custom formatters own their internal work and allocation.
func (d Query[Q]) Encode(ctx context.Context, input Q, limits QueryLimits) (string, error) {
	if err := d.prepare(ctx, limits); err != nil {
		return "", err
	}
	values := make(url.Values)
	budget := queryBudget{pairs: limits.Pairs, bytes: limits.Bytes}
	var failed *queryBindingFailure
	var info QueryParameterInfo
	err := callback.Isolated("encode query parameters", func() error {
		for _, parameter := range d.parameters {
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
		return "", &QueryError{cause: err}
	}
	return encoded, nil
}

func queryCallbackResult(ctx context.Context, callbackErr error, failed *queryBindingFailure, info QueryParameterInfo) error {
	// No arbitrary error methods are invoked to classify codec failures.
	if callbackErr != nil {
		return fault.Wrap(fault.Internal, "query binding callback failed", callbackErr)
	}
	if failed != nil && failed.internal {
		return fault.Wrap(fault.Internal, "query field binding failed", failed.cause)
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
	return &QueryError{issues: []contract.Issue{{Path: path, Code: contract.ValueIssue}}, cause: failed.cause}
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
