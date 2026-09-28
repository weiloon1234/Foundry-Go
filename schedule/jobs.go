package schedule

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/model"
)

// JobTarget preserves definition routing. Each occurrence becomes a stable
// queue execution ID; retention must cover the recovery/catch-up window.
func JobTarget[P any](dispatcher *jobs.Dispatcher, definition jobs.Definition[P], build func(context.Context, Invocation) (P, error)) (Handler, error) {
	if dispatcher == nil {
		return nil, fault.New(fault.Invalid, "schedule job target requires dispatcher")
	}
	if err := definition.Validate(); err != nil {
		return nil, err
	}
	return jobTarget(build, func(ctx context.Context, payload P, options jobs.Options[P]) error {
		_, err := definition.Dispatch(ctx, dispatcher, payload, options)
		return err
	})
}

// ConnectionJobTarget selects the connection's configured default queue. The
// bound job and explicit options retain the concrete payload type throughout.
func ConnectionJobTarget[P any](connection *jobs.Connection, definition jobs.Definition[P], queue jobs.Queue, build func(context.Context, Invocation) (P, error)) (Handler, error) {
	bound, err := definition.On(connection)
	if err != nil {
		return nil, err
	}
	if queue != "" {
		if err := queue.Validate(); err != nil {
			return nil, err
		}
	}
	return jobTarget(build, func(ctx context.Context, payload P, options jobs.Options[P]) error {
		options.Queue = queue
		_, err := bound.Dispatch(ctx, payload, options)
		return err
	})
}
func jobTarget[P any](build func(context.Context, Invocation) (P, error), dispatch func(context.Context, P, jobs.Options[P]) error) (Handler, error) {
	if build == nil {
		return nil, fault.New(fault.Invalid, "schedule job target requires payload builder")
	}
	return func(ctx context.Context, invocation Invocation) error {
		if invocation.Occurrence.IsZero() {
			return fault.New(fault.Invalid, "scheduled job requires an occurrence identity")
		}
		payload, err := build(ctx, invocation)
		if err != nil {
			return err
		}
		id := model.IDFromBytes[jobs.ExecutionOf[P]](invocation.Occurrence.Bytes())
		return dispatch(ctx, payload, jobs.Options[P]{ID: id})
	}, nil
}
