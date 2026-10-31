package websocket

import (
	"encoding/json"
	"unicode"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/model"
)

const ProtocolVersion = 1
const Subprotocol = "foundry.v1"
const MaxRoomBytes = 512
const MaxReplayMessages = 1024

// ChannelID identifies a declared channel in the wire protocol.
type ChannelID string

// EventID identifies an event within a channel.
type EventID string

// RequestID correlates a client request with its response.
type RequestID string

// Connection identifies the model type for a WebSocket connection.
type Connection struct{}

// ConnectionID identifies a WebSocket connection.
type ConnectionID = model.ID[Connection]

// Publication identifies the model type for a publication.
type Publication struct{}

// MessageID identifies a publication.
type MessageID = model.ID[Publication]

// Action identifies a client request operation.
type Action string

const (
	Subscribe   Action = "subscribe"
	Unsubscribe Action = "unsubscribe"
	Message     Action = "message"
)

// Request is the wire boundary only. Normal domain handlers receive typed
// MessageContext and payload values; no JSON map inspection is necessary.
type Request struct {
	Version int             `json:"v"`
	Action  Action          `json:"action"`
	ID      RequestID       `json:"id"`
	Channel ChannelID       `json:"channel"`
	Room    *string         `json:"room,omitempty"`
	Event   EventID         `json:"event,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Replay  *int            `json:"replay,omitempty"`
}

// Code identifies a protocol failure sent to clients.
type Code string

const (
	Malformed          Code = "malformed"
	UnsupportedVersion Code = "unsupported_version"
	UnknownChannel     Code = "unknown_channel"
	UnknownEvent       Code = "unknown_event"
	WrongDirection     Code = "wrong_direction"
	Unauthenticated    Code = "unauthenticated"
	Forbidden          Code = "forbidden"
	NotSubscribed      Code = "not_subscribed"
	AlreadySubscribed  Code = "already_subscribed"
	InvalidPayload     Code = "invalid_payload"
	CapacityExceeded   Code = "capacity_exceeded"
	OperationFailed    Code = "operation_failed"
	OperationTimedOut  Code = "operation_timed_out"
	Stopping           Code = "stopping"
	RateLimited        Code = "rate_limited"
	// Unavailable is a retryable transient failure, such as a cluster
	// authority timeout or exhausted management capacity.
	Unavailable Code = "unavailable"
)

// Error returns the protocol error code text.
func (c Code) Error() string { return string(c) }

// ResponseType identifies the kind of server wire response.
type ResponseType string

const (
	Subscribed      ResponseType = "subscribed"
	Unsubscribed    ResponseType = "unsubscribed"
	Acknowledged    ResponseType = "ack"
	Accepted        ResponseType = "accepted"
	ErrorResponse   ResponseType = "error"
	EventResponse   ResponseType = "event"
	PresenceJoined  ResponseType = "presence_joined"
	PresenceLeft    ResponseType = "presence_left"
	PresenceUpdated ResponseType = "presence_updated"
)

// MemberID identifies a presence subject using an opaque digest.
type MemberID string

// MemberFrame carries a presence member's ID, JSON data and connection count on the
// wire.
type MemberFrame struct {
	ID          MemberID        `json:"id"`
	Data        json.RawMessage `json:"data"`
	Connections int             `json:"connections"`
}

// Response is the server wire envelope for acknowledgements, errors, events and
// presence updates.
type Response struct {
	Version   int             `json:"v"`
	Type      ResponseType    `json:"type"`
	ID        RequestID       `json:"id,omitempty"`
	Channel   ChannelID       `json:"channel,omitempty"`
	Room      *string         `json:"room,omitempty"`
	Event     EventID         `json:"event,omitempty"`
	MessageID MessageID       `json:"message_id,omitzero"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Code      Code            `json:"code,omitempty"`
	Members   []MemberFrame   `json:"members,omitempty"`
	Member    *MemberFrame    `json:"member,omitempty"`
	Replayed  bool            `json:"replayed,omitempty"`
}

func validRoom(room string) bool {
	if room == "" || len(room) > MaxRoomBytes || !utf8.ValidString(room) {
		return false
	}
	for _, r := range room {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// DecodeRequest enforces the exact versioned envelope in one strict pass:
// duplicate/unknown members, invalid UTF-8 and unpaired surrogates are rejected.
// Callers must bound frame buffering first. A malformed request still reports
// its ID when one was decoded and valid, so errors can correlate.
func DecodeRequest(data []byte, maxBytes int) (Request, error) {
	if maxBytes < 1 || maxBytes > 1<<20 {
		return Request{}, codeFailure(Malformed)
	}
	var request Request
	present, err := decodeObject(data, maxBytes, func(name string) wireMember {
		switch name {
		case "v":
			return into(&request.Version)
		case "action":
			return into(&request.Action)
		case "id":
			return into(&request.ID)
		case "channel":
			return into(&request.Channel)
		case "room":
			return into(&request.Room)
		case "event":
			return into(&request.Event)
		case "payload":
			return into(&request.Payload)
		case "replay":
			return into(&request.Replay)
		}
		return nil
	})
	identified := Request{}
	if identifier.Semantic(string(request.ID)) {
		identified.ID = request.ID
	}
	if err != nil || !identifier.Semantic(string(request.ID)) || !identifier.Semantic(string(request.Channel)) {
		return identified, codeFailure(Malformed)
	}
	if request.Version != ProtocolVersion {
		return request, codeFailure(UnsupportedVersion)
	}
	if present["replay"] && (request.Action != Subscribe || request.Replay == nil || *request.Replay < 0 || *request.Replay > MaxReplayMessages) {
		return request, codeFailure(Malformed)
	}
	if present["room"] && (request.Room == nil || !validRoom(*request.Room)) {
		return request, codeFailure(Malformed)
	}
	switch request.Action {
	case Subscribe, Unsubscribe:
		if present["event"] || present["payload"] {
			return request, codeFailure(Malformed)
		}
	case Message:
		if !identifier.Semantic(string(request.Event)) || len(request.Payload) == 0 {
			return request, codeFailure(Malformed)
		}
	default:
		return request, codeFailure(Malformed)
	}
	return request, nil
}
