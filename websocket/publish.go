package websocket

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/model"
)

// Publish reaches subscriptions for exactly this room. Whole-channel subscribers
// do not implicitly subscribe to every private room. Success means local queue
// admission or distributed live publication was attempted, not recipient delivery.
// Slow peers disconnect. Distributed errors may follow partial publication/history.
func Publish[C, R, S, P any](ctx context.Context, source PublisherSource, channel Channel[C, R, S], room R, event Outgoing[C, P], payload P) (MessageID, error) {
	return publish(ctx, source, channel, event, payload, Room(room))
}

// Broadcast reaches every connection subscribed anywhere in this channel, once
// per connection even when it has overlapping whole-channel/room subscriptions.
func Broadcast[C, R, S, P any](ctx context.Context, source PublisherSource, channel Channel[C, R, S], event Outgoing[C, P], payload P) (MessageID, error) {
	return publish(ctx, source, channel, event, payload, WholeChannel[R]())
}
func publish[C, R, S, P any](ctx context.Context, source PublisherSource, channel Channel[C, R, S], event Outgoing[C, P], payload P, target Target[R]) (MessageID, error) {
	if ctx == nil {
		return MessageID{}, fault.New(fault.Invalid, "WebSocket publication requires a context")
	}
	hub, err := publicationHub(source)
	if err != nil {
		return MessageID{}, err
	}
	if err := hub.validateChannel(channel.token, channel.id); err != nil {
		return MessageID{}, err
	}
	d := event.definition
	if d == nil || event.encode == nil || d.channel != channel.token || hub.registry.channels[channel.id].events[d.id] != d || d.direction != ServerToClient {
		return MessageID{}, fault.New(fault.Invalid, "outgoing event declaration is not registered on this channel")
	}
	ctx, finish, err := hub.operation(ctx)
	if err != nil {
		return MessageID{}, err
	}
	defer finish()
	var id MessageID
	err = callback.Isolated("WebSocket publication", func() error {
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
			return hub.publishDistributed(ctx, response, frame)
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
		hub.routePublicationLocked(response, frame)
		hub.metrics[channel.id].Published++
		hub.publications++
		return nil
	})
	if err != nil {
		return MessageID{}, err
	}
	return id, nil
}
