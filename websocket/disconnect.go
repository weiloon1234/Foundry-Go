package websocket

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/model"
)

// DisconnectConnection is trusted server-side management. Distributed delivery
// uses live pub/sub; an error never proves that no instance acted on the command.
func DisconnectConnection(ctx context.Context, source PublisherSource, id ConnectionID) error {
	if id.IsZero() {
		return fault.New(fault.Invalid, "disconnect requires a connection identity")
	}
	hub, err := publicationHub(source)
	if err != nil {
		return err
	}
	ctx, finish, err := hub.operation(ctx)
	if err != nil {
		return err
	}
	defer finish()
	hub.disconnectConnection(id)
	if hub.cluster != nil {
		return hub.sendClusterControl(ctx, clusterEnvelope{Kind: clusterDisconnectConnection, Connection: id})
	}
	return nil
}

// DisconnectSubject preserves the model/key type plus guard/provider namespace.
// Supply the generated reference containing the stored key, not a presentation
// getter. Call after committed revocation; periodic fresh authorization remains
// the backstop if a live disconnect message is lost.
func DisconnectSubject[M model.Identifiable, K any](ctx context.Context, source PublisherSource, guard auth.Guard[M], reference model.Reference[M, K]) error {
	hub, err := publicationHub(source)
	if err != nil {
		return err
	}
	ctx, finish, err := hub.operation(ctx)
	if err != nil {
		return err
	}
	defer finish()
	return callback.Isolated("WebSocket subject disconnect", func() error {
		if err := guard.Validate(); err != nil {
			return err
		}
		identity, err := reference.Identity()
		if err != nil {
			return err
		}
		subject, err := memberID(SubjectReference{Guard: guard.Name(), Provider: guard.ProviderName(), Identity: identity})
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		hub.disconnectSubject(subject)
		if hub.cluster != nil {
			return hub.sendClusterControl(ctx, clusterEnvelope{Kind: clusterDisconnectSubject, Subject: subject})
		}
		return nil
	})
}
func (h *Hub) disconnectConnection(id ConnectionID) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if c := h.connections[id]; c != nil && c.ctx.Err() == nil {
		h.forcedDisconnects++
		c.cancel()
	}
}
func (h *Hub) disconnectSubject(id MemberID) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range h.connections {
		if c.ctx.Err() != nil {
			continue
		}
		matched := false
		for _, subscription := range c.subscriptions {
			if subscription.subjectID == id {
				matched = true
				break
			}
		}
		if !matched {
			for _, pending := range c.pending {
				if pending.subscription.subjectID == id {
					matched = true
					break
				}
			}
		}
		if matched {
			h.forcedDisconnects++
			c.cancel()
		}
	}
}
