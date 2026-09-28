package http

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

// Preparation receives structurally decoded values and returns query/body values
// for validation. Path identity cannot be replaced. Treat input and all referenced
// data as borrowed and immutable; copy a slice/map before changing its contents.
// Do not perform business writes here. Defaults require Optional wire fields.
type Preparation[P, Q, B any] func(context.Context, Input[P, Q, B]) (Q, B, error)

// RequestAuthorization runs after preparation and validation, before handler or
// model-binding work. Return a declared HTTP error to deny the request safely.
type RequestAuthorization[P, Q, B any] func(context.Context, Input[P, Q, B]) error

// WithPreparation replaces the optional preparation callback on an independent
// endpoint. An explicitly nil callback is invalid at registration. Prohibited and
// Absent rules also check original input before this callback can transform it.
func (e Endpoint[P, Q, B, R]) WithPreparation(prepare Preparation[P, Q, B]) Endpoint[P, Q, B, R] {
	e.preparation = &prepare
	return e
}

// WithAuthorization replaces the optional request authorization callback. Use an
// authenticated endpoint's version when authorization needs a concrete actor.
func (e Endpoint[P, Q, B, R]) WithAuthorization(authorize RequestAuthorization[P, Q, B]) Endpoint[P, Q, B, R] {
	e.authorization = &authorize
	return e
}

// Capture returned errors separately: callback containment must never format an
// application error. Cancellation retains callback/resource ownership until exit.
func requestHook(ctx context.Context, operation string, fn func() error) error {
	if err := ctx.Err(); err != nil {
		return RequestTimeout.WithCause(err)
	}
	var returned error
	failure := callback.Isolated(operation, func() error { returned = fn(); return nil })
	if failure != nil {
		return InternalError.WithCause(failure)
	}
	if err := ctx.Err(); err != nil {
		return RequestTimeout.WithCause(err)
	}
	return returned
}
