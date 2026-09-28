package redis

import (
	"context"
	_ "embed"
	"encoding/json"
	"slices"

	driver "github.com/redis/go-redis/v9"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
	"github.com/weiloon1234/Foundry-Go/pubsub"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

//go:embed websocket.lua
var websocketScript string

// WebSocketBackend borrows the existing Redis pool and pub/sub implementation.
// It owns no server/connection lifecycle and never retries a state mutation.
// Keep its Client alive until every borrowing Hub/Publisher has actually stopped.
type WebSocketBackend struct{ client *Client }

func NewWebSocketBackend(client *Client) (*WebSocketBackend, error) {
	if client == nil || client.done == nil {
		return nil, fault.New(fault.Invalid, "WebSocket Redis backend requires a prepared client")
	}
	return &WebSocketBackend{client: client}, nil
}

var _ websocket.ClusterBackend = (*WebSocketBackend)(nil)

func (b *WebSocketBackend) Publish(ctx context.Context, channel pubsub.Channel, data []byte) (uint64, error) {
	if b == nil || b.client == nil {
		return 0, fault.New(fault.Invalid, "WebSocket Redis backend is not initialized")
	}
	return b.client.Publish(ctx, channel, data)
}
func (b *WebSocketBackend) Subscribe(ctx context.Context, channels []pubsub.Channel, limits pubsub.Limits) (pubsub.Stream, error) {
	if b == nil || b.client == nil {
		return nil, fault.New(fault.Invalid, "WebSocket Redis backend is not initialized")
	}
	return b.client.Subscribe(ctx, channels, limits)
}

type websocketRequest struct {
	Op             string `json:"op"`
	Instance       string `json:"instance"`
	Connection     string `json:"connection"`
	Scope          string `json:"scope"`
	Subject        string `json:"subject"`
	Presence       bool   `json:"presence"`
	Data           string `json:"data"`
	Channel        string `json:"channel"`
	Frame          string `json:"frame"`
	Message        string `json:"message"`
	ReplayMessages int    `json:"replay_messages"`
	ReplayBytes    int    `json:"replay_bytes"`
	ReplayTTL      int64  `json:"replay_ttl"`
}
type websocketReply struct {
	Revision uint64 `json:"revision"`
	Members  []struct {
		ID          websocket.MemberID `json:"id"`
		Data        string             `json:"data"`
		Connections int                `json:"connections"`
	} `json:"members"`
	Frames []string `json:"frames"`
}

func (b *WebSocketBackend) command(ctx context.Context, key websocket.ClusterKey, request websocketRequest) (websocketReply, error) {
	if err := websocket.ValidateClusterOperation(ctx, key); err != nil {
		return websocketReply{}, err
	}
	if b == nil || b.client == nil {
		return websocketReply{}, fault.New(fault.Invalid, "WebSocket Redis backend is not initialized")
	}
	limits := key.Limits()
	bounds, err := json.Marshal(struct {
		Connections   int   `json:"connections"`
		Subjects      int   `json:"subjects"`
		Subscriptions int   `json:"subscriptions"`
		Members       int   `json:"members"`
		MemberBytes   int   `json:"member_bytes"`
		FrameBytes    int   `json:"frame_bytes"`
		TTL           int64 `json:"ttl"`
		Retention     int64 `json:"retention"`
	}{limits.Connections, limits.ConnectionsPerSubject, limits.Subscriptions, limits.PresenceMembers, limits.MemberBytes, limits.FrameBytes, limits.ConnectionTTL.Milliseconds(), limits.Retention.Milliseconds()})
	if err != nil {
		return websocketReply{}, err
	}
	data, err := json.Marshal(request)
	if err != nil {
		return websocketReply{}, err
	}
	raw, err := b.client.execute(ctx, func(ctx context.Context, client *driver.Client) (any, error) {
		return client.Eval(ctx, websocketScript, []string{key.String() + ":metadata", key.String() + ":connections"}, key.String(), key.Policy(), string(bounds), string(data)).Result()
	})
	if err != nil {
		return websocketReply{}, err
	}
	parts, ok := raw.([]any)
	if !ok || len(parts) < 1 || len(parts) > 2 {
		return websocketReply{}, fault.New(fault.Internal, "invalid Redis WebSocket reply")
	}
	status, ok := parts[0].(int64)
	if !ok {
		return websocketReply{}, fault.New(fault.Internal, "invalid Redis WebSocket status")
	}
	switch status {
	case -1:
		return websocketReply{}, fault.New(fault.Invalid, "stored Redis WebSocket state is corrupt")
	case -2:
		return websocketReply{}, fault.New(fault.Conflict, "live Redis WebSocket policy differs")
	case -3:
		return websocketReply{}, websocket.CapacityExceeded
	case -4:
		return websocketReply{}, websocket.Stopping
	case -5:
		return websocketReply{}, fault.New(fault.Conflict, "Redis WebSocket clock or revision is outside its bound")
	case 0:
	default:
		return websocketReply{}, fault.New(fault.Internal, "unknown Redis WebSocket status")
	}
	if len(parts) == 1 {
		return websocketReply{}, nil
	}
	encoded, ok := parts[1].(string)
	// Application JSON remains an opaque string through cjson. Escaping can
	// expand each byte sixfold; both member and replay budgets are explicit.
	maximum := max(limits.PresenceMembers*(6*limits.MemberBytes+256), 6*request.ReplayBytes) + 16384
	if !ok || len(encoded) > maximum {
		return websocketReply{}, fault.New(fault.Invalid, "Redis WebSocket result exceeds its bound")
	}
	if _, err := jsonwire.Decode([]byte(encoded), jsonwire.Limits{Bytes: maximum, Depth: 8, Nodes: 65536}); err != nil {
		return websocketReply{}, err
	}
	var result websocketReply
	if err := json.Unmarshal([]byte(encoded), &result); err != nil {
		return websocketReply{}, fault.New(fault.Invalid, "invalid Redis WebSocket data")
	}
	if len(result.Members) > limits.PresenceMembers || len(result.Frames) > request.ReplayMessages {
		return websocketReply{}, fault.New(fault.Invalid, "Redis WebSocket result exceeds its collection bound")
	}
	return result, nil
}
func (b *WebSocketBackend) connection(ctx context.Context, key websocket.ClusterKey, instance websocket.InstanceID, connection websocket.ConnectionID, op string) (websocketReply, error) {
	if err := websocket.ValidateClusterConnection(ctx, key, instance, connection); err != nil {
		return websocketReply{}, err
	}
	return b.command(ctx, key, websocketRequest{Op: op, Instance: instance.String(), Connection: connection.String()})
}
func (b *WebSocketBackend) WebSocketCheck(ctx context.Context, key websocket.ClusterKey) error {
	_, err := b.command(ctx, key, websocketRequest{Op: "check"})
	return err
}
func (b *WebSocketBackend) WebSocketOpen(ctx context.Context, key websocket.ClusterKey, instance websocket.InstanceID, connection websocket.ConnectionID) error {
	_, err := b.connection(ctx, key, instance, connection, "open")
	return err
}
func (b *WebSocketBackend) WebSocketTouch(ctx context.Context, key websocket.ClusterKey, instance websocket.InstanceID, connection websocket.ConnectionID) error {
	_, err := b.connection(ctx, key, instance, connection, "touch")
	return err
}
func (b *WebSocketBackend) WebSocketClose(ctx context.Context, key websocket.ClusterKey, instance websocket.InstanceID, connection websocket.ConnectionID) error {
	_, err := b.connection(ctx, key, instance, connection, "close")
	return err
}
func (b *WebSocketBackend) WebSocketJoin(ctx context.Context, key websocket.ClusterKey, instance websocket.InstanceID, connection websocket.ConnectionID, membership websocket.ClusterMembership) (websocket.PresenceSnapshot, error) {
	if err := websocket.ValidateClusterConnection(ctx, key, instance, connection); err != nil {
		return websocket.PresenceSnapshot{}, err
	}
	if err := membership.Validate(key.Limits()); err != nil {
		return websocket.PresenceSnapshot{}, err
	}
	result, err := b.command(ctx, key, websocketRequest{Op: "join", Instance: instance.String(), Connection: connection.String(), Scope: membership.Scope.Hash(), Subject: string(membership.Subject), Presence: membership.Presence, Data: string(membership.Data)})
	if err != nil {
		return websocket.PresenceSnapshot{}, err
	}
	return websocketPresence(result, key)
}
func (b *WebSocketBackend) WebSocketLeave(ctx context.Context, key websocket.ClusterKey, instance websocket.InstanceID, connection websocket.ConnectionID, scope websocket.Scope) error {
	if err := websocket.ValidateClusterConnection(ctx, key, instance, connection); err != nil {
		return err
	}
	if err := scope.Validate(); err != nil {
		return err
	}
	_, err := b.command(ctx, key, websocketRequest{Op: "leave", Instance: instance.String(), Connection: connection.String(), Scope: scope.Hash()})
	return err
}
func (b *WebSocketBackend) WebSocketMembers(ctx context.Context, key websocket.ClusterKey, scope websocket.Scope) (websocket.PresenceSnapshot, error) {
	if err := scope.Validate(); err != nil {
		return websocket.PresenceSnapshot{}, err
	}
	result, err := b.command(ctx, key, websocketRequest{Op: "members", Scope: scope.Hash()})
	if err != nil {
		return websocket.PresenceSnapshot{}, err
	}
	return websocketPresence(result, key)
}
func websocketPresence(result websocketReply, key websocket.ClusterKey) (websocket.PresenceSnapshot, error) {
	snapshot := websocket.PresenceSnapshot{Revision: result.Revision, Members: make([]websocket.MemberFrame, 0, len(result.Members))}
	if result.Revision > 1<<53-1 {
		return websocket.PresenceSnapshot{}, fault.New(fault.Invalid, "Redis presence revision exceeds its bound")
	}
	for _, member := range result.Members {
		if member.Connections < 1 || member.Connections > key.Limits().Connections || len(member.Data) > key.Limits().MemberBytes {
			return websocket.PresenceSnapshot{}, fault.New(fault.Invalid, "Redis presence member exceeds its bound")
		}
		snapshot.Members = append(snapshot.Members, websocket.MemberFrame{ID: member.ID, Data: json.RawMessage(member.Data), Connections: member.Connections})
	}
	return snapshot, nil
}
func replayRequest(channel websocket.ChannelID, policy websocket.ReplayConfig) (websocketRequest, error) {
	if !identifier.Semantic(string(channel)) {
		return websocketRequest{}, fault.New(fault.Invalid, "invalid WebSocket history channel")
	}
	if err := policy.Validate(); err != nil {
		return websocketRequest{}, err
	}
	return websocketRequest{Channel: string(channel), ReplayMessages: policy.Messages, ReplayBytes: policy.Bytes, ReplayTTL: policy.TTL.Milliseconds()}, nil
}
func (b *WebSocketBackend) WebSocketAppend(ctx context.Context, key websocket.ClusterKey, channel websocket.ChannelID, policy websocket.ReplayConfig, data []byte) error {
	if policy.TTL > key.Limits().Retention {
		return fault.New(fault.Invalid, "replay retention exceeds cluster policy")
	}
	request, err := replayRequest(channel, policy)
	if err != nil {
		return err
	}
	response, err := websocket.DecodePublication(data, key.Limits().FrameBytes)
	if err != nil {
		return err
	}
	if response.Channel != channel || policy.Messages > 0 && len(data) > policy.Bytes {
		return fault.New(fault.Invalid, "invalid WebSocket history publication")
	}
	request.Op = "append"
	request.Frame = string(data)
	request.Message = response.MessageID.String()
	_, err = b.command(ctx, key, request)
	return err
}
func (b *WebSocketBackend) WebSocketHistory(ctx context.Context, key websocket.ClusterKey, channel websocket.ChannelID, policy websocket.ReplayConfig) ([][]byte, error) {
	if policy.TTL > key.Limits().Retention {
		return nil, fault.New(fault.Invalid, "replay retention exceeds cluster policy")
	}
	request, err := replayRequest(channel, policy)
	if err != nil {
		return nil, err
	}
	request.Op = "history"
	reply, err := b.command(ctx, key, request)
	if err != nil {
		return nil, err
	}
	result := make([][]byte, 0, len(reply.Frames))
	size := 0
	for _, frame := range reply.Frames {
		size += len(frame)
		if size > policy.Bytes || len(frame) > key.Limits().FrameBytes {
			return nil, fault.New(fault.Invalid, "Redis replay payload exceeds its bound")
		}
		result = append(result, slices.Clone([]byte(frame)))
	}
	return result, nil
}
