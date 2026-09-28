package websocket

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
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
func (h *Hub) publishDistributed(ctx context.Context, response Response, data []byte) error {
	err := h.clusterCall(ctx, func(ctx context.Context) error {
		if err := h.cluster.backend.WebSocketAppend(ctx, h.cluster.key, response.Channel, h.registry.channels[response.Channel].replay, data); err != nil {
			return err
		}
		return h.publishEnvelope(ctx, clusterEnvelope{Kind: clusterPublication, Frame: data})
	})
	if err == nil {
		h.mu.Lock()
		h.publications++
		h.metrics[response.Channel].Published++
		h.mu.Unlock()
	}
	return err
}
func decodeExact(data []byte, maximum int, output any) error {
	node, err := jsonwire.Decode(data, jsonwire.Limits{Bytes: maximum, Depth: jsonwire.MaxDepth, Nodes: 65536})
	if err != nil {
		return err
	}
	object, ok := node.(map[string]any)
	typ := reflect.TypeOf(output)
	if !ok || typ == nil || typ.Kind() != reflect.Pointer || typ.Elem().Kind() != reflect.Struct {
		return fault.New(fault.Invalid, "wire envelope requires an object")
	}
	typ = typ.Elem()
	names := make(map[string]bool, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		name, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			names[name] = true
		}
	}
	for name := range object {
		if !names[name] {
			return fault.New(fault.Invalid, "unknown wire envelope property")
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(output)
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
	var envelope clusterEnvelope
	if err := decodeExact(data, h.cluster.config.Buffer.PayloadBytes, &envelope); err != nil {
		return err
	}
	if envelope.Version != ProtocolVersion || envelope.Policy != h.cluster.key.Policy() || envelope.Instance.IsZero() {
		return fault.New(fault.Conflict, "WebSocket cluster protocol or registry policy differs")
	}
	switch envelope.Kind {
	case clusterPublication:
		if !envelope.Connection.IsZero() || envelope.Subject != "" || envelope.Scope != nil {
			return fault.New(fault.Invalid, "invalid publication envelope")
		}
		response, err := h.decodePublication(ctx, envelope.Frame)
		if err != nil {
			return err
		}
		h.mu.Lock()
		defer h.mu.Unlock()
		if !h.closing {
			h.routePublicationLocked(response, envelope.Frame)
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
		return h.refreshClusterPresence(ctx, *envelope.Scope)
	default:
		return fault.New(fault.Invalid, "unknown cluster command")
	}
}

// DecodePublication validates the stable event envelope for adapters and history.
// Runtime registries additionally validate the registered room and payload types.
func DecodePublication(data []byte, maxBytes int) (Response, error) {
	if maxBytes < 1 || maxBytes > 1<<20 {
		return Response{}, fault.New(fault.Invalid, "invalid publication frame bound")
	}
	var response Response
	if err := decodeExact(data, maxBytes, &response); err != nil {
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
