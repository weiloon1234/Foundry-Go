package websocket

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/model"
)

// PublishOption adjusts one publication's live delivery.
type PublishOption func(*publishOptions)
type publishOptions struct{ except ConnectionID }

// ExceptConnection skips live delivery to one connection, typically the sender
// of a relayed message ("to others"). Recent replay still includes the event.
func ExceptConnection(id ConnectionID) PublishOption {
	return func(o *publishOptions) { o.except = id }
}

// Publish reaches subscriptions for exactly this room. Whole-channel subscribers
// do not implicitly subscribe to every private room. Success means local queue
// admission or distributed live publication was attempted, not recipient delivery.
// Slow peers disconnect. Distributed errors may follow partial publication/history.
func Publish[C, R, S, P any](ctx context.Context, source PublisherSource, channel Channel[C, R, S], room R, event Outgoing[C, P], payload P, options ...PublishOption) (MessageID, error) {
	return publish(ctx, source, channel, event, payload, Room(room), options)
}

// Broadcast reaches every connection subscribed anywhere in this channel, once
// per connection even when it has overlapping whole-channel/room subscriptions.
func Broadcast[C, R, S, P any](ctx context.Context, source PublisherSource, channel Channel[C, R, S], event Outgoing[C, P], payload P, options ...PublishOption) (MessageID, error) {
	return publish(ctx, source, channel, event, payload, WholeChannel[R](), options)
}

// NotPublished reports that a Publish or Broadcast error happened before any
// live publication, fan-out or replay history was attempted (validation, local
// admission, a closing hub or payload encoding), so retrying cannot produce a
// duplicate. Other errors may follow partial distributed publication.
func NotPublished(err error) bool { return errorgraph.Has[*notPublished](err) }

type notPublished struct{ cause error }

func (e *notPublished) Error() string { return e.cause.Error() }
func (e *notPublished) Unwrap() error { return e.cause }

func publish[C, R, S, P any](ctx context.Context, source PublisherSource, channel Channel[C, R, S], event Outgoing[C, P], payload P, target Target[R], options []PublishOption) (MessageID, error) {
	id, attempted, err := publishAttempt(ctx, source, channel, event, payload, target, options)
	if err != nil && !attempted {
		return MessageID{}, &notPublished{err}
	}
	return id, err
}

// publishAttempt reports whether publication itself was attempted: after that
// point an error may follow partial distributed publication or history.
func publishAttempt[C, R, S, P any](ctx context.Context, source PublisherSource, channel Channel[C, R, S], event Outgoing[C, P], payload P, target Target[R], options []PublishOption) (id MessageID, attempted bool, err error) {
	var settings publishOptions
	for _, option := range options {
		if option == nil {
			return MessageID{}, false, fault.New(fault.Invalid, "nil WebSocket publish option")
		}
		option(&settings)
	}
	if ctx == nil {
		return MessageID{}, false, fault.New(fault.Invalid, "WebSocket publication requires a context")
	}
	hub, err := publicationHub(source)
	if err != nil {
		return MessageID{}, false, err
	}
	if err := hub.validateChannel(channel.token, channel.id); err != nil {
		return MessageID{}, false, err
	}
	d := event.definition
	if d == nil || event.encode == nil || d.channel != channel.token || hub.registry.channels[channel.id].events[d.id] != d || d.direction != ServerToClient {
		return MessageID{}, false, fault.New(fault.Invalid, "outgoing event declaration is not registered on this channel")
	}
	ctx, finish, err := hub.operation(ctx)
	if err != nil {
		return MessageID{}, false, err
	}
	defer finish()
	// The typed codecs below contain their own panics; publication is a
	// framework step and needs no additional isolation goroutine.
	err = callback.Invoke("WebSocket publication", func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		key := subscriptionKey{channel: channel.id}
		if room, ok := target.Room.Get(); ok {
			text, err := channel.rooms.encode(ctx, room)
			if err != nil {
				return err
			}
			key.room = text
			key.hasRoom = true
		}
		data, err := event.encode(ctx, payload, hub.config.Payload)
		if err != nil {
			return err
		}
		id, err = model.NewID[Publication]()
		if err != nil {
			return err
		}
		response := Response{Type: EventResponse, Channel: key.channel, Room: key.roomPointer(), Event: d.id, MessageID: id, Payload: data}
		frame, err := hub.encode(response)
		if err != nil {
			return err
		}
		if hub.cluster != nil {
			attempted = true
			return hub.publishDistributed(ctx, response, frame, settings.except)
		}
		hub.mu.Lock()
		defer hub.mu.Unlock()
		if err := ctx.Err(); err != nil {
			return err
		}
		if hub.closing {
			return Stopping
		}
		if err := hub.retainLocked(response, frame); err != nil {
			return err
		}
		attempted = true
		hub.routePublicationLocked(response, frame, settings.except)
		hub.metrics[channel.id].published.Add(1)
		hub.counters.publications.Add(1)
		return nil
	})
	if err != nil {
		return MessageID{}, attempted, err
	}
	return id, true, nil
}
