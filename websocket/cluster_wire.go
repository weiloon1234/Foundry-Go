package websocket

import (
	"context"
	"encoding/json"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
)

type clusterKind string

const (
	clusterPublication          clusterKind = "publication"
	clusterDisconnectConnection clusterKind = "disconnect_connection"
	clusterDisconnectSubject    clusterKind = "disconnect_subject"
	clusterPresenceChanged      clusterKind = "presence_changed"
)

type clusterEnvelope struct {
	Version  int         `json:"v"`
	Policy   string      `json:"policy"`
	Instance InstanceID  `json:"instance"`
	Kind     clusterKind `json:"kind"`
	// Base64 keeps the maximum frame expansion predictable and does not add a
	// second JSON nesting budget to the separately validated event frame.
	Frame      []byte       `json:"frame,omitempty"`
	Connection ConnectionID `json:"connection,omitzero"`
	Subject    MemberID     `json:"subject,omitempty"`
	Scope      *Scope       `json:"scope,omitempty"`
}

func (h *Hub) publishEnvelope(ctx context.Context, envelope clusterEnvelope) error {
	envelope.Version = ProtocolVersion
	envelope.Policy = h.cluster.key.Policy()
	envelope.Instance = h.cluster.instance
	data, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	if len(data) > h.cluster.config.Buffer.PayloadBytes {
		return fault.New(fault.Invalid, "cluster envelope exceeds its transport bound")
	}
	_, err = h.cluster.backend.Publish(ctx, h.cluster.topic, data)
	return err
}
func (h *Hub) sendClusterControl(ctx context.Context, envelope clusterEnvelope) error {
	return h.clusterCall(ctx, func(ctx context.Context) error {
		if err := h.cluster.backend.WebSocketCheck(ctx, h.cluster.key); err != nil {
			return err
		}
		return h.publishEnvelope(ctx, envelope)
	})
}
func (h *Hub) publishDistributed(ctx context.Context, response Response, data []byte, except ConnectionID) error {
	policy := h.registry.channels[response.Channel].replay
	if !except.IsZero() && !h.cluster.config.ExcludeRemoteConnections {
		// Earlier releases reject envelopes that carry an exclusion, so a local
		// connection is excluded when this instance routes the echo instead.
		if err := h.excludeLocally(response.MessageID, except); err != nil {
			return &notPublished{err}
		}
		except = ConnectionID{}
	}
	err := h.clusterCall(ctx, func(ctx context.Context) error {
		// History is written only for channels that retain replay.
		if policy.Messages > 0 {
			if err := h.cluster.backend.WebSocketAppend(ctx, h.cluster.key, response.Channel, policy, data); err != nil {
				return err
			}
		}
		// Connection names an excluded live recipient on publications.
		return h.publishEnvelope(ctx, clusterEnvelope{Kind: clusterPublication, Frame: data, Connection: except})
	})
	if err == nil {
		h.counters.publications.Add(1)
		h.metrics[response.Channel].published.Add(1)
	}
	// A successful exclusion is consumed by the echo, which may arrive after
	// this returns; expiry removes one whose echo is lost.
	return err
}

// excludeLocally records an exclusion of this instance's own connection for
// the publication's echo. A connection hosted elsewhere needs the envelope
// field, which requires ExcludeRemoteConnections across the namespace.
func (h *Hub) excludeLocally(id MessageID, connection ConnectionID) error {
	now := time.Now()
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, local := h.connections[connection]; !local {
		return fault.New(fault.Invalid, "excluding a connection of another instance requires ClusterConfig.ExcludeRemoteConnections")
	}
	state := h.cluster
	expired := 0
	for expired < len(state.exclusionOrder) && !now.Before(state.exclusionOrder[expired].expires) {
		delete(state.exclusions, state.exclusionOrder[expired].id)
		expired++
	}
	state.exclusionOrder = state.exclusionOrder[expired:]
	if len(state.exclusions) >= maxLocalExclusions {
		return fault.New(fault.Overloaded, "too many publications await their exclusion echo")
	}
	state.exclusions[id] = connection
	state.exclusionOrder = append(state.exclusionOrder, pendingExclusion{id: id, expires: now.Add(exclusionLifetime)})
	return nil
}

// decodeEnvelope decodes a cluster envelope once, including its nested scope.
func decodeEnvelope(data []byte, maximum int) (clusterEnvelope, error) {
	var envelope clusterEnvelope
	_, err := decodeObject(data, maximum, func(name string) wireMember {
		switch name {
		case "v":
			return into(&envelope.Version)
		case "policy":
			return into(&envelope.Policy)
		case "instance":
			return into(&envelope.Instance)
		case "kind":
			return into(&envelope.Kind)
		case "frame":
			return into(&envelope.Frame)
		case "connection":
			return into(&envelope.Connection)
		case "subject":
			return into(&envelope.Subject)
		case "scope":
			return func(decoder *json.Decoder) error {
				var raw json.RawMessage
				if err := decoder.Decode(&raw); err != nil {
					return err
				}
				var scope Scope
				_, err := decodeObject(raw, len(raw), func(name string) wireMember {
					switch name {
					case "channel":
						return into(&scope.Channel)
					case "room":
						return into(&scope.Room)
					case "has_room":
						return into(&scope.HasRoom)
					}
					return nil
				})
				envelope.Scope = &scope
				return err
			}
		}
		return nil
	})
	return envelope, err
}
func (h *Hub) decodePublication(ctx context.Context, data []byte) (Response, error) {
	response, err := DecodePublication(data, h.config.MaxFrameBytes)
	if err != nil {
		return Response{}, err
	}
	channel := h.registry.channels[response.Channel]
	if channel == nil {
		return Response{}, UnknownChannel
	}
	event := channel.events[response.Event]
	if event == nil || event.direction != ServerToClient || event.wire == nil {
		return Response{}, WrongDirection
	}
	if err := channel.room(ctx, response.Room); err != nil {
		return Response{}, err
	}
	if err := event.wire(ctx, response.Payload, h.config.Payload); err != nil {
		return Response{}, err
	}
	return response, nil
}
func (h *Hub) receiveCluster(ctx context.Context, data []byte) error {
	envelope, err := decodeEnvelope(data, h.cluster.config.Buffer.PayloadBytes)
	if err != nil {
		return err
	}
	if envelope.Version != ProtocolVersion || envelope.Policy != h.cluster.key.Policy() || envelope.Instance.IsZero() {
		return fault.New(fault.Conflict, "WebSocket cluster protocol or registry policy differs")
	}
	switch envelope.Kind {
	case clusterPublication:
		if envelope.Subject != "" || envelope.Scope != nil {
			return fault.New(fault.Invalid, "invalid publication envelope")
		}
		response, err := h.decodePublication(ctx, envelope.Frame)
		if err != nil {
			return err
		}
		h.mu.Lock()
		defer h.mu.Unlock()
		except := envelope.Connection
		if local, ok := h.cluster.exclusions[response.MessageID]; ok && envelope.Instance == h.cluster.instance {
			except = local
			delete(h.cluster.exclusions, response.MessageID)
		}
		if !h.closing {
			h.routePublicationLocked(response, envelope.Frame, except)
		}
		return nil
	case clusterDisconnectConnection:
		if len(envelope.Frame) != 0 || envelope.Connection.IsZero() || envelope.Subject != "" || envelope.Scope != nil {
			return fault.New(fault.Invalid, "invalid disconnect envelope")
		}
		h.disconnectConnection(envelope.Connection)
		return nil
	case clusterDisconnectSubject:
		if len(envelope.Frame) != 0 || !envelope.Connection.IsZero() || !validDigest(string(envelope.Subject)) || envelope.Scope != nil {
			return fault.New(fault.Invalid, "invalid subject command")
		}
		h.disconnectSubject(envelope.Subject)
		return nil
	case clusterPresenceChanged:
		if len(envelope.Frame) != 0 || !envelope.Connection.IsZero() || envelope.Subject != "" || envelope.Scope == nil {
			return fault.New(fault.Invalid, "invalid presence command")
		}
		if err := envelope.Scope.Validate(); err != nil {
			return err
		}
		// The worker refreshes from the authority off this receive loop.
		h.mu.Lock()
		marked := h.markPresenceLocked(envelope.Scope.key())
		h.mu.Unlock()
		if marked {
			h.wakePresence()
		}
		return nil
	default:
		return fault.New(fault.Invalid, "unknown cluster command")
	}
}

// DecodePublication validates the stable event envelope for adapters and history
// in one strict pass. Runtime registries additionally validate the registered
// room and payload types.
func DecodePublication(data []byte, maxBytes int) (Response, error) {
	if maxBytes < 1 || maxBytes > 1<<20 {
		return Response{}, fault.New(fault.Invalid, "invalid publication frame bound")
	}
	var response Response
	if _, err := decodeObject(data, maxBytes, func(name string) wireMember {
		switch name {
		case "v":
			return into(&response.Version)
		case "type":
			return into(&response.Type)
		case "id":
			return into(&response.ID)
		case "channel":
			return into(&response.Channel)
		case "room":
			return into(&response.Room)
		case "event":
			return into(&response.Event)
		case "message_id":
			return into(&response.MessageID)
		case "payload":
			return into(&response.Payload)
		case "code":
			return into(&response.Code)
		case "members":
			return into(&response.Members)
		case "member":
			return into(&response.Member)
		case "replayed":
			return into(&response.Replayed)
		}
		return nil
	}); err != nil {
		return Response{}, err
	}
	if response.Version != ProtocolVersion || response.Type != EventResponse || response.MessageID.IsZero() || response.ID != "" || response.Code != "" || len(response.Members) != 0 || response.Member != nil || response.Replayed {
		return Response{}, fault.New(fault.Invalid, "invalid distributed publication")
	}

	if !identifier.Semantic(string(response.Channel)) || !identifier.Semantic(string(response.Event)) || len(response.Payload) == 0 || response.Room != nil && !validRoom(*response.Room) {
		return Response{}, fault.New(fault.Invalid, "invalid publication identifiers")
	}
	return response, nil
}
