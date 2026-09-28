package http

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

// credentialResponse shares the explicit disclosure boundary for opaque auth
// results. Only a framework adapter can opt into secure POST/no-store handling.
func credentialResponse[T, W any](status int, wire contract.JSON[W], validate func() error, prepare func(context.Context, T) (W, error)) Response[T] {
	return Response[T]{kind: payloadJSON, status: status, credentials: true,
		json: credentialResponseJSON[T, W]{wire: wire, validate: validate, prepare: prepare}}
}

type credentialResponseJSON[T, W any] struct {
	wire     contract.JSON[W]
	validate func() error
	prepare  func(context.Context, T) (W, error)
}

func (d credentialResponseJSON[T, W]) Validate() error {
	if d.prepare == nil {
		return fault.New(fault.Invalid, "credential response requires a preparation callback")
	}
	if d.validate != nil {
		if err := d.validate(); err != nil {
			return err
		}
	}
	return d.wire.Validate()
}
func (d credentialResponseJSON[T, W]) Description() (contract.Schema, error) {
	if err := d.Validate(); err != nil {
		return contract.Schema{}, err
	}
	return d.wire.Description()
}
func (d credentialResponseJSON[T, W]) Encode(ctx context.Context, result T, limits contract.JSONLimits) ([]byte, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, fault.New(fault.Invalid, "credential delivery requires a context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var payload W
	err := callback.Isolated("prepare credential response", func() error {
		var err error
		payload, err = d.prepare(ctx, result)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return d.wire.Encode(ctx, payload, limits)
}
func credentialClockValid(source clock.Clock) error {
	if nilCookieValue(source) {
		return fault.New(fault.Invalid, "credential response requires a clock")
	}
	return nil
}
func credentialTime(source clock.Clock) (temporal.DateTime, error) {
	now, err := temporal.NewDateTime(source.Now())
	if err != nil || now.IsZero() {
		return temporal.DateTime{}, fault.New(fault.Invalid, "credential delivery clock is invalid")
	}
	return now, nil
}
