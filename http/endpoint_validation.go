package http

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/validation"
)

// WithValidation adds request rules in declaration order, including comparisons
// across parameter sources. It returns an independent endpoint. Rules execute
// after decoding and before the handler; they must not mutate input values.
func (e Endpoint[P, Q, B, R]) WithValidation(rules ...validation.Rule[Input[P, Q, B]]) Endpoint[P, Q, B, R] {
	rule := validation.All(rules...)
	if e.validation != nil {
		rule = validation.All(*e.validation, rule)
	}
	e.validation = &rule
	return e
}

// WithPathValidation adds rules for the concrete path value. Diagnostics and
// metadata retain the path source prefix, including declared nested fields.
func (e Endpoint[P, Q, B, R]) WithPathValidation(rules ...validation.Rule[P]) Endpoint[P, Q, B, R] {
	field := validation.DefineField("path", func(input Input[P, Q, B]) P { return input.Path })
	return e.WithValidation(field.Rules(rules...))
}

// WithQueryValidation adds rules for the concrete query value.
func (e Endpoint[P, Q, B, R]) WithQueryValidation(rules ...validation.Rule[Q]) Endpoint[P, Q, B, R] {
	field := validation.DefineField("query", func(input Input[P, Q, B]) Q { return input.Query })
	return e.WithValidation(field.Rules(rules...))
}

// WithBodyValidation adds rules for the decoded body. Use generated field
// declarations and Optional/Nullable rules to retain wire names and presence.
func (e Endpoint[P, Q, B, R]) WithBodyValidation(rules ...validation.Rule[B]) Endpoint[P, Q, B, R] {
	field := validation.DefineField("body", func(input Input[P, Q, B]) B { return input.Body })
	return e.WithValidation(field.Rules(rules...))
}

func endpointValidationError(ctx context.Context, err error) error {
	if canceled := ctx.Err(); canceled != nil && err == canceled {
		return RequestTimeout.WithCause(err)
	}
	switch err.(type) {
	case *validation.Errors:
		return err
	case *validation.LimitError:
		return BadRequest.WithCause(err)
	default:
		return InternalError.WithCause(err)
	}
}
