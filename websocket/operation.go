package websocket

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/maintenance"
	"github.com/weiloon1234/Foundry-Go/observability"
)

func (c *connectionState) process(request Request) {
	base := c.ctx
	if request.Action == Message {
		// Messages reuse the connection's current authentication scope and each
		// subscription's cached authorization within the freshness window.
		scope, release, err := c.messageScope()
		if err != nil {
			c.hub.counters.failures.Add(1)
			c.respond(Response{Type: ErrorResponse, ID: request.ID, Channel: request.Channel, Room: request.Room, Code: Unauthenticated})
			c.revoke()
			return
		}
		defer release()
		if scope != nil {
			base = scope.Context()
		}
	}
	ctx, cancel := context.WithTimeout(base, c.hub.config.OperationTimeout)
	defer cancel()
	name := observability.Name("unknown")
	if c.hub.registry.channels[request.Channel] != nil {
		name = observability.Name(request.Channel)
	}
	observed, span, _ := observability.FromContext(ctx).Start(ctx, observability.Operation{Kind: observability.SocketMessage, Name: name})
	if observed != nil {
		ctx = observed
	}
	outcome := observability.Panicked
	defer func() { span.End(observability.Result{Outcome: outcome}) }()
	ctx, finish := ownedContext(ctx, c.hub)
	defer finish()
	metric := c.hub.metrics[request.Channel]
	if metric != nil {
		metric.incoming.Add(1)
	}
	var response Response
	var code Code
	failed := callback.Isolated("WebSocket operation", func() error {
		if request.Action != Unsubscribe && maintenance.FromContext(ctx).Admit() != nil {
			code = Stopping
			return nil
		}
		var err error
		response, err = c.dispatch(ctx, request)
		code = operationCode(err)
		return nil
	})
	if failed != nil {
		code = OperationFailed
	}
	outcome = socketOutcome(code)
	if failed != nil {
		outcome = observability.Panicked
	}
	if code == "" && response.Type == "" {
		return
	} // Subscribe queued its result at admission.
	if ctx.Err() != nil {
		code = OperationTimedOut
		outcome = observability.OutcomeFor(ctx.Err())
	}
	if code != "" {
		c.hub.counters.failures.Add(1)
		if metric != nil {
			metric.failures.Add(1)
		}
		response = Response{Type: ErrorResponse, Code: code}
	}
	if response.Type == Acknowledged && metric != nil {
		metric.completed.Add(1)
	}
	response.ID = request.ID
	response.Channel = request.Channel
	response.Room = request.Room
	c.respond(response)
}

func socketOutcome(code Code) observability.Outcome {
	switch code {
	case "":
		return observability.Succeeded
	case OperationFailed:
		return observability.Failed
	case OperationTimedOut:
		return observability.TimedOut
	default:
		return observability.Rejected
	}
}

// This classifier runs inside callback.Isolated: arbitrary error Is/Unwrap may
// execute code. One bounded walk preserves code priority across joined errors;
// incomplete inspection fails conservatively. No underlying error text or panic
// data reaches the wire.
func operationCode(err error) Code {
	if err == nil {
		return ""
	}
	candidates := [...]struct {
		target error
		code   Code
	}{
		{Malformed, Malformed}, {UnknownChannel, UnknownChannel},
		{UnknownEvent, UnknownEvent}, {WrongDirection, WrongDirection},
		{Unauthenticated, Unauthenticated}, {Forbidden, Forbidden},
		{NotSubscribed, NotSubscribed}, {AlreadySubscribed, AlreadySubscribed},
		{InvalidPayload, InvalidPayload}, {CapacityExceeded, CapacityExceeded},
		{Unavailable, Unavailable}, {fault.Overloaded, Unavailable},
		{OperationFailed, OperationFailed}, {OperationTimedOut, OperationTimedOut},
		{Stopping, Stopping}, {auth.Unauthenticated, Unauthenticated},
		{auth.MFARequired, Unauthenticated}, {auth.Forbidden, Forbidden},
		{context.Canceled, OperationTimedOut}, {context.DeadlineExceeded, OperationTimedOut},
	}
	best := len(candidates)
	complete := errorgraph.Walk(err, func(current error) bool {
		for i := 0; i < best; i++ {
			if errorgraph.Matches(current, candidates[i].target) {
				best = i
				break
			}
		}
		return best != 0
	})
	if !complete || best == len(candidates) {
		return OperationFailed
	}
	return candidates[best].code
}
func (c *connectionState) dispatch(ctx context.Context, request Request) (Response, error) {
	channel := c.hub.registry.channels[request.Channel]
	if channel == nil {
		return Response{}, UnknownChannel
	}
	key := requestKey(request)
	if request.Action == Unsubscribe {
		c.hub.mu.Lock()
		subscription := c.subscriptions[key]
		if subscription != nil {
			c.removeLocked(subscription)
		}
		c.hub.mu.Unlock()
		if subscription == nil {
			return Response{}, NotSubscribed
		}
		if err := c.leave(subscription); err != nil {
			return Response{}, err
		}
		return Response{Type: Unsubscribed}, nil
	}
	if request.Action == Subscribe {
		// Admission always authorizes with freshly resolved credentials.
		var reply Response
		err := c.withFreshScope(ctx, func(ctx context.Context) error {
			access, err := channel.check(ctx, request.Room)
			if err != nil {
				return err
			}
			reply, err = c.subscribe(access.context, request, key, channel, access)
			return err
		})
		return reply, err
	}
	c.hub.mu.Lock()
	subscription := c.subscriptions[key]
	closing := c.hub.closing
	var access accessResult
	if subscription != nil {
		access = subscription.access
	}
	c.hub.mu.Unlock()
	if closing {
		return Response{}, Stopping
	}
	if subscription == nil {
		return Response{}, NotSubscribed
	}
	if channel.private {
		var err error
		if ctx, err = attribution.WithContext(ctx, access.origin); err != nil {
			return Response{}, err
		}
	}
	access.context = ctx
	event := channel.events[request.Event]
	if event == nil {
		return Response{}, UnknownEvent
	}
	if event.direction != ClientToServer {
		return Response{}, WrongDirection
	}
	if err := event.invoke(ctx, c.hub, c.id, access, request.Payload, c.hub.config.Payload, func() bool {
		accepted := c.respond(Response{Type: Accepted, ID: request.ID, Channel: request.Channel, Room: request.Room})
		if accepted {
			c.hub.metrics[request.Channel].accepted.Add(1)
		}
		return accepted
	}); err != nil {
		return Response{}, err
	}
	if err := ctx.Err(); err != nil {
		return Response{}, err
	}
	return Response{Type: Acknowledged}, nil
}
