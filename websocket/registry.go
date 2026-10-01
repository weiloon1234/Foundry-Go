package websocket

import (
	"cmp"
	"context"
	"slices"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
)

type EventInfo struct {
	ID                      EventID         `json:"id"`
	Direction               Direction       `json:"direction"`
	Dynamic                 bool            `json:"dynamic"`
	AcceptedAcknowledgement bool            `json:"accepted_acknowledgement"`
	Payload                 contract.Schema `json:"payload,omitzero"`
}
type ChannelInfo struct {
	ID         ChannelID                 `json:"id"`
	Room       foundryhttp.URLScalarInfo `json:"room"`
	Private    bool                      `json:"private"`
	OwnedRooms bool                      `json:"owned_rooms"`
	Guard      auth.GuardName            `json:"guard,omitempty"`
	Provider   auth.ProviderName         `json:"provider,omitempty"`
	Presence   *contract.Schema          `json:"presence,omitempty"`
	Replay     ReplayConfig              `json:"replay,omitzero"`
	Events     []EventInfo               `json:"events"`
}
type Registration struct{ definition *channelDefinition }

const MaxChannels = 256
const MaxChannelEvents = 256

type channelDefinition struct {
	memberWire   func(context.Context, []byte, contract.JSONLimits) error
	token        *channelToken
	id           ChannelID
	private      bool
	replay       ReplayConfig
	presence     *presenceToken
	validate     func() error
	validateAuth func(*auth.Registry) error
	check        func(context.Context, *string) (accessResult, error)
	room         func(context.Context, *string) error
	join         func(context.Context, *Hub, ConnectionID, accessResult) error
	leave        func(context.Context, *Hub, ConnectionID, *string, SubjectReference) error
	member       func(context.Context, accessResult, contract.JSONLimits) ([]byte, error)
	description  func() ChannelInfo
	events       map[EventID]*eventDefinition
}

// Register is the only erasure boundary. Event ownership is checked by the Go
// compiler and by nominal declaration identity when the registry is built.
func Register[C, R, S any](channel Channel[C, R, S], events ...EventRegistration[C]) Registration {
	owned := slices.Clone(events)
	d := &channelDefinition{token: channel.token, id: channel.id, private: channel.private, check: channel.check, replay: channel.replay}
	d.room = func(ctx context.Context, room *string) error { _, err := channel.rooms.decode(ctx, room); return err }
	d.validate = func() error {
		if err := channel.Validate(); err != nil {
			return err
		}
		if channel.presence != nil {
			if err := outputPresentation(channel.presence.description, "presence"); err != nil {
				return err
			}
		}
		if len(owned) > MaxChannelEvents {
			return fault.New(fault.Invalid, "too many channel events")
		}
		seen := make(map[EventID]bool)
		for _, item := range owned {
			e := item.definition
			if e == nil || e.token == nil || e.channel != channel.token || !identifier.Semantic(string(e.id)) || e.validate == nil || e.description == nil || (e.direction != ClientToServer && e.direction != ServerToClient) {
				return fault.New(fault.Invalid, "invalid channel event registration")
			}
			if seen[e.id] {
				return fault.New(fault.Duplicate, "channel event already registered")
			}
			seen[e.id] = true
			if err := e.validate(); err != nil {
				return err
			}
			if e.direction == ServerToClient && !e.dynamic {
				if err := outputPresentation(e.description, "event"); err != nil {
					return err
				}
			}
		}
		for _, item := range owned {
			if target := item.definition.relay; target != nil {
				found := false
				for _, candidate := range owned {
					if candidate.definition == target {
						found = true
						break
					}
				}
				if !found {
					return fault.New(fault.Missing, "relay outgoing event is not registered")
				}
			}
		}
		return nil
	}
	d.validateAuth = func(registry *auth.Registry) error {
		if channel.private {
			return channel.guard.ValidateIn(registry)
		}
		return nil
	}
	d.join = func(ctx context.Context, hub *Hub, id ConnectionID, result accessResult) error {
		if channel.hooks.Joined == nil {
			return nil
		}
		return channel.hooks.Joined(ctx, MessageContext[R, S]{Publisher: hub, Connection: id, Target: result.target.(Target[R]), Subject: result.subject.(S)})
	}
	d.leave = func(ctx context.Context, hub *Hub, id ConnectionID, room *string, subject SubjectReference) error {
		if channel.hooks.Left == nil {
			return nil
		}
		target, err := channel.rooms.decode(ctx, room)
		if err != nil {
			return err
		}
		return channel.hooks.Left(ctx, LeaveContext[R]{Publisher: hub, Connection: id, Target: target, Subject: subject})
	}
	if channel.presence != nil {
		d.presence = channel.presence.token
		d.memberWire = channel.presence.verify
		d.member = func(ctx context.Context, result accessResult, limits contract.JSONLimits) ([]byte, error) {
			return channel.presence.encode(ctx, result.subject.(S), limits)
		}
	}
	d.events = make(map[EventID]*eventDefinition, len(owned))
	for _, item := range owned {
		if item.definition != nil {
			d.events[item.definition.id] = item.definition
		}
	}
	d.description = func() ChannelInfo {
		room, _ := channel.rooms.Description()
		info := ChannelInfo{ID: channel.id, Room: room, Private: channel.private, OwnedRooms: channel.owned, Events: make([]EventInfo, 0, len(owned))}
		info.Replay = channel.replay
		if channel.private {
			info.Guard = channel.guard.Name()
			info.Provider = channel.guard.ProviderName()
		}
		if channel.presence != nil {
			schema, _ := channel.presence.description()
			info.Presence = &schema
		}
		for _, item := range owned {
			e := item.definition
			schema, _ := e.description()
			info.Events = append(info.Events, EventInfo{ID: e.id, Direction: e.direction, Dynamic: e.dynamic, AcceptedAcknowledgement: e.accepted, Payload: schema})
		}
		slices.SortFunc(info.Events, func(a, b EventInfo) int { return cmp.Compare(a.ID, b.ID) })
		return info
	}
	return Registration{definition: d}
}

type Registry struct {
	channels map[ChannelID]*channelDefinition
	ordered  []*channelDefinition
}

// outputPresentation rejects a server-sent payload whose graph reaches a
// password hint. Password hints are input-only; the registry checks each
// payload once, whether or not clients are exported.
func outputPresentation(describe func() (contract.Schema, error), output string) error {
	schema, err := describe()
	if err != nil {
		return err
	}
	return contract.RejectPasswordOutput(schema, output)
}

func NewRegistry(registrations ...Registration) (*Registry, error) {
	if len(registrations) < 1 || len(registrations) > MaxChannels {
		return nil, fault.New(fault.Invalid, "WebSocket registry needs 1 to 256 channels")
	}
	r := &Registry{channels: make(map[ChannelID]*channelDefinition, len(registrations))}
	for _, registration := range registrations {
		d := registration.definition
		if d == nil || d.validate == nil {
			return nil, fault.New(fault.Invalid, "invalid channel registration")
		}
		if err := d.validate(); err != nil {
			return nil, err
		}
		if r.channels[d.id] != nil {
			return nil, fault.New(fault.Duplicate, "channel already registered")
		}
		r.channels[d.id] = d
		r.ordered = append(r.ordered, d)
	}
	slices.SortFunc(r.ordered, func(a, b *channelDefinition) int { return cmp.Compare(a.id, b.id) })
	return r, nil
}

// Channels returns deterministic owned contract snapshots. Milestone21 exporters
// consume these descriptors; no second payload or room schema is maintained.
func (r *Registry) Channels() []ChannelInfo {
	if r == nil {
		return nil
	}
	result := make([]ChannelInfo, 0, len(r.ordered))
	for _, d := range r.ordered {
		result = append(result, d.description())
	}
	return result
}
