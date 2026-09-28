package websocket

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// PublisherSource is implemented by a socket Hub and a server-side Publisher.
// The same typed declaration/room/payload boundary serves HTTP, jobs and sockets.
type PublisherSource interface{ publicationHub() *Hub }

func (h *Hub) publicationHub() *Hub { return h }

// Publisher owns publication operations without a socket listener or auth scopes.
// Its constructor binds the distributed backend and immutable registry.
type Publisher struct{ hub *Hub }

func (p *Publisher) publicationHub() *Hub {
	if p == nil {
		return nil
	}
	return p.hub
}
func (p *Publisher) Close(ctx context.Context) error {
	if p == nil {
		return fault.New(fault.Invalid, "WebSocket publisher is not initialized")
	}
	return p.hub.Stop(ctx)
}
func publicationHub(source PublisherSource) (*Hub, error) {
	if source == nil {
		return nil, fault.New(fault.Invalid, "WebSocket publication requires a publisher")
	}
	hub := source.publicationHub()
	if hub == nil || hub.done == nil {
		return nil, fault.New(fault.Invalid, "WebSocket publisher is not initialized")
	}
	return hub, nil
}
