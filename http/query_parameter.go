package http

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// QueryParameter binds one declared query name to a concrete field. Construct
// it with QueryParam, OptionalQueryParam or RepeatedQueryParam. Field selectors
// must select a field on the supplied argument and be safe for concurrent calls
// on independent arguments. They must not mutate any other field or retain it.
type QueryParameter[Q any] struct {
	info   QueryParameterInfo
	err    error
	scalar func() (URLScalarInfo, error)
	decode func(context.Context, *Q, []string) *queryBindingFailure
	encode func(context.Context, *Q, *queryBudget) ([]string, *queryBindingFailure)
}

// QueryParameterInfo describes cardinality and codec metadata without inspecting values.
// Repeated fields use repeated keys, never implicit comma or bracket expansion.
type QueryParameterInfo struct {
	Scalar   *URLScalarInfo `json:"scalar,omitempty"`
	Name     string         `json:"name"`
	Required bool           `json:"required"`
	Repeated bool           `json:"repeated"`
	// DefaultURL is an unescaped URL scalar spelling, not a JSON default value.
	// Absence differs from a present empty-string default.
	DefaultURL value.Optional[string] `json:"default_url,omitzero"`
}

type queryBindingFailure struct {
	cause    error
	index    int
	internal bool
}

func querySelectorFailure() *queryBindingFailure {
	return &queryBindingFailure{cause: fault.New(fault.Internal, "query field selector returned nil"), internal: true}
}

// QueryParam requires exactly one value, including its explicit zero or empty
// representation. A duplicate scalar key is rejected before any codec executes.
func QueryParam[Q, V any](name string, codec QueryCodec[V], field func(*Q) *V) QueryParameter[Q] {
	info := QueryParameterInfo{Name: name, Required: true}
	if field == nil {
		return QueryParameter[Q]{info: info}
	}
	return bindQueryParameter(info, codec, func(query *Q) ([]V, bool) {
		if source := field(query); source != nil {
			return []V{*source}, true
		}
		return nil, false
	}, func(query *Q, values []V) bool {
		if destination := field(query); destination != nil {
			*destination = values[0]
			return true
		}
		return false
	})
}

// OptionalQueryParam distinguishes omission from an explicitly supplied zero
// or empty value. Query strings have no implicit database/JSON null literal.
func OptionalQueryParam[Q, V any](name string, codec QueryCodec[V], field func(*Q) *value.Optional[V]) QueryParameter[Q] {
	info := QueryParameterInfo{Name: name}
	if field == nil {
		return QueryParameter[Q]{info: info}
	}
	return bindQueryParameter(info, codec, func(query *Q) ([]V, bool) {
		if source := field(query); source != nil {
			if item, present := source.Get(); present {
				return []V{item}, true
			}
			return nil, true
		}
		return nil, false
	}, func(query *Q, values []V) bool {
		if destination := field(query); destination != nil {
			*destination = value.Optional[V]{}
			if len(values) != 0 {
				*destination = value.Set(values[0])
			}
			return true
		}
		return false
	})
}

// RepeatedQueryParam binds repeated keys to an ordinary or named slice. An
// omitted key produces nil; a present empty value is one item for the codec to
// parse. Nil and empty slices both encode as omission. Item order is preserved.
func RepeatedQueryParam[Q, V any, S ~[]V](name string, codec QueryCodec[V], field func(*Q) *S) QueryParameter[Q] {
	info := QueryParameterInfo{Name: name, Repeated: true}
	if field == nil {
		return QueryParameter[Q]{info: info}
	}
	return bindQueryParameter(info, codec, func(query *Q) ([]V, bool) {
		if source := field(query); source != nil {
			return []V(*source), true
		}
		return nil, false
	}, func(query *Q, values []V) bool {
		if destination := field(query); destination != nil {
			*destination = S(values)
			return true
		}
		return false
	})
}

func bindQueryParameter[Q, V any](info QueryParameterInfo, codec QueryCodec[V], read func(*Q) ([]V, bool), write func(*Q, []V) bool) QueryParameter[Q] {
	// Reuse the same concrete scalar and typed-nil codec checks as path binding.
	// The identity selector introduces no path grammar or URL escaping.
	scalar := Param(info.Name, codec, func(item *V) *V { return item })
	if scalar.decode == nil || scalar.encode == nil {
		return QueryParameter[Q]{info: info}
	}
	return QueryParameter[Q]{info: info, scalar: scalar.scalar,
		decode: func(ctx context.Context, query *Q, text []string) *queryBindingFailure {
			var decoded []V
			if len(text) != 0 {
				decoded = make([]V, len(text))
			}
			for i, item := range text {
				if err := ctx.Err(); err != nil {
					return &queryBindingFailure{cause: err, index: i}
				}
				if err := scalar.decode(&decoded[i], item); err != nil {
					return &queryBindingFailure{cause: err, index: i}
				}
			}
			if !write(query, decoded) {
				return querySelectorFailure()
			}
			return nil
		},
		encode: func(ctx context.Context, query *Q, budget *queryBudget) ([]string, *queryBindingFailure) {
			items, ok := read(query)
			if !ok {
				return nil, querySelectorFailure()
			}
			if len(items) > budget.pairs {
				return nil, &queryBindingFailure{cause: queryLimitFailure()}
			}
			var encoded []string
			if len(items) != 0 {
				encoded = make([]string, 0, len(items))
			}
			for i, item := range items {
				if err := ctx.Err(); err != nil {
					return nil, &queryBindingFailure{cause: err, index: i}
				}
				text, err := scalar.encode(&item)
				if err == nil {
					err = budget.take(info.Name, text)
				}
				if err != nil {
					return nil, &queryBindingFailure{cause: err, index: i}
				}
				encoded = append(encoded, text)
			}
			return encoded, nil
		},
	}
}

type queryBudget struct{ pairs, bytes int }

func queryLimitFailure() error { return fault.New(fault.Invalid, "query transport limit exceeded") }

func (b *queryBudget) take(name, text string) error {
	if b.pairs == 0 || len(name) > b.bytes || len(text) > b.bytes-len(name) {
		return queryLimitFailure()
	}
	b.pairs--
	b.bytes -= len(name) + len(text)
	return nil
}
