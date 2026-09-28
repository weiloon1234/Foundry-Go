package websocket

import (
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"time"
)

type ProtocolActions struct {
	Subscribe   Action `json:"subscribe"`
	Unsubscribe Action `json:"unsubscribe"`
	Message     Action `json:"message"`
}
type ProtocolResponses struct {
	Subscribed      ResponseType `json:"subscribed"`
	Unsubscribed    ResponseType `json:"unsubscribed"`
	Acknowledged    ResponseType `json:"acknowledged"`
	Accepted        ResponseType `json:"accepted"`
	Error           ResponseType `json:"error"`
	Event           ResponseType `json:"event"`
	PresenceJoined  ResponseType `json:"presence_joined"`
	PresenceLeft    ResponseType `json:"presence_left"`
	PresenceUpdated ResponseType `json:"presence_updated"`
}

// ProtocolInfo is the versioned transport vocabulary used by runtime and client
// exporters. It contains no live connection state or credentials.
type ProtocolInfo struct {
	Version           int               `json:"version"`
	Subprotocol       string            `json:"subprotocol"`
	MaxRoomBytes      int               `json:"max_room_bytes"`
	MaxReplayMessages int               `json:"max_replay_messages"`
	Actions           ProtocolActions   `json:"actions"`
	Responses         ProtocolResponses `json:"responses"`
	Codes             []Code            `json:"codes"`
}

func ProtocolDescription() ProtocolInfo {
	return ProtocolInfo{
		Version: ProtocolVersion, Subprotocol: Subprotocol, MaxRoomBytes: MaxRoomBytes, MaxReplayMessages: MaxReplayMessages,
		Actions:   ProtocolActions{Subscribe, Unsubscribe, Message},
		Responses: ProtocolResponses{Subscribed, Unsubscribed, Acknowledged, Accepted, ErrorResponse, EventResponse, PresenceJoined, PresenceLeft, PresenceUpdated},
		Codes:     []Code{Malformed, UnsupportedVersion, UnknownChannel, UnknownEvent, WrongDirection, Unauthenticated, Forbidden, NotSubscribed, AlreadySubscribed, InvalidPayload, CapacityExceeded, OperationFailed, OperationTimedOut, Stopping, RateLimited},
	}
}

// ClientLimits exports the actual configured public budgets. Time is expressed
// in milliseconds rounded up; server policy/credential material remains private.
type ClientLimits struct {
	Subscriptions         int                 `json:"subscriptions"`
	FrameBytes            int                 `json:"frame_bytes"`
	PresenceMembers       int                 `json:"presence_members"`
	MemberBytes           int                 `json:"member_bytes"`
	DeduplicationEntries  int                 `json:"deduplication_entries"`
	OperationMilliseconds int64               `json:"operation_ms"`
	Payload               contract.JSONLimits `json:"payload"`
}

type ClientDescription struct {
	Protocol ProtocolInfo  `json:"protocol"`
	Limits   ClientLimits  `json:"limits"`
	Channels []ChannelInfo `json:"channels"`
}

// Validate reuses runtime configuration validation with the smallest private
// capacity scope. Public metadata cannot infer a server's private connection
// counts, origins or credential refresh policy.
func (limits ClientLimits) Validate() error {
	if limits.OperationMilliseconds < 1 || limits.OperationMilliseconds > int64(time.Minute/time.Millisecond) {
		return fault.New(fault.Invalid, "invalid client operation bound")
	}
	config := DefaultConfig()
	config.MaxConnections, config.MaxPresenceScopes = 1, 1
	config.MaxSubscriptions, config.MaxFrameBytes = limits.Subscriptions, limits.FrameBytes
	config.MaxPresenceMembers, config.MaxMemberBytes = limits.PresenceMembers, limits.MemberBytes
	config.DeduplicationEntries = limits.DeduplicationEntries
	config.OperationTimeout = time.Duration(limits.OperationMilliseconds) * time.Millisecond
	config.Payload = limits.Payload
	return config.Validate()
}

// DescribeClient uses the same registry/config supplied to New or
// NewDistributed. It starts no hub, opens no sockets and invokes no handlers.
func DescribeClient(registry *Registry, config Config) (ClientDescription, error) {
	if registry == nil {
		return ClientDescription{}, fault.New(fault.Invalid, "client contract requires a WebSocket registry")
	}
	if err := config.Validate(); err != nil {
		return ClientDescription{}, err
	}
	return ClientDescription{
		Protocol: ProtocolDescription(), Channels: registry.Channels(),
		Limits: ClientLimits{
			Subscriptions: config.MaxSubscriptions, FrameBytes: config.MaxFrameBytes,
			PresenceMembers: config.MaxPresenceMembers, MemberBytes: config.MaxMemberBytes,
			DeduplicationEntries:  config.DeduplicationEntries,
			OperationMilliseconds: int64((config.OperationTimeout + time.Millisecond - 1) / time.Millisecond), Payload: config.Payload,
		},
	}, nil
}
