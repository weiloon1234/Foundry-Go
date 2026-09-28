package websocket

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/observability"
)

func (c *connectionState) process(request Request) {
	ctx, cancel := context.WithTimeout(c.ctx, c.hub.config.OperationTimeout)
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
	c.hub.mu.Lock()
	if metric := c.hub.metrics[request.Channel]; metric != nil {
		metric.Incoming++
	}
	c.hub.mu.Unlock()
	var response Response
	var code Code
	failed := callback.Isolated("WebSocket operation", func() error {
		if request.Action != Unsubscribe && observability.FromContext(ctx).Gate().Admit() != nil {
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
		c.hub.mu.Lock()
		c.hub.failures++
		if metric := c.hub.metrics[request.Channel]; metric != nil {
			metric.Failures++
		}
		c.hub.mu.Unlock()
		response = Response{Type: ErrorResponse, Code: code}
	}
	if response.Type == Acknowledged {
		c.hub.mu.Lock()
		c.hub.metrics[request.Channel].Completed++
		c.hub.mu.Unlock()
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
	var reply Response
	err := c.withFreshScope(ctx, func(ctx context.Context) error {
		var err error
		reply, err = c.dispatchAuthorized(ctx, request, key, channel)
		return err
	})
	return reply, err
}
func (c *connectionState) dispatchAuthorized(ctx context.Context, request Request, key subscriptionKey, channel *channelDefinition) (Response, error) {
	access, err := channel.check(ctx, request.Room)
	if err != nil {
		return Response{}, err
	}
	ctx = access.context
	if request.Action == Subscribe {
		return c.subscribe(ctx, request, key, channel, access)
	}
	c.hub.mu.Lock()
	subscribed := c.subscriptions[key] != nil
	closing := c.hub.closing
	c.hub.mu.Unlock()
	if closing {
		return Response{}, Stopping
	}
	if !subscribed {
		return Response{}, NotSubscribed
	}
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
			c.hub.mu.Lock()
			c.hub.metrics[request.Channel].Accepted++
			c.hub.mu.Unlock()
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
