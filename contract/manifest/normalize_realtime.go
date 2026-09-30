package manifest

import (
	"cmp"
	"reflect"
	"slices"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

func normalizeRealtime(realtime *Realtime, types typeIndex) error {
	if realtime == nil {
		return nil
	}
	if !reflect.DeepEqual(realtime.Protocol, websocket.ProtocolDescription()) {
		return invalid("unsupported realtime protocol contract")
	}
	if err := realtime.Limits.Validate(); err != nil {
		return err
	}
	if len(realtime.Channels) < 1 || len(realtime.Channels) > websocket.MaxChannels {
		return invalid("invalid realtime channel count")
	}
	ids, names := make(map[websocket.ChannelID]bool), make(map[string]bool)
	for i := range realtime.Channels {
		channel := &realtime.Channels[i]
		if channel.Events == nil {
			channel.Events = []Event{}
		}
		name, err := clientName(string(channel.ID))
		if err != nil {
			return err
		}
		if channel.Name != name || ids[channel.ID] || names[name] {
			return invalid("duplicate channel or client name")
		}
		ids[channel.ID], names[name] = true, true
		if channel.Room.Name != "room" || channel.Room.Required != channel.OwnedRooms || channel.Room.Repeated || channel.Room.CatchAll || channel.Room.DefaultURL.IsSet() {
			return invalid("invalid realtime room contract")
		}
		if err := types.urlParameter(channel.Room); err != nil {
			return err
		}
		if channel.Private {
			if !identifier.Semantic(string(channel.Guard)) || !identifier.Semantic(string(channel.Provider)) {
				return invalid("private channel has no guard/provider")
			}
		} else if channel.Guard != "" || channel.Provider != "" || channel.OwnedRooms {
			return invalid("public channel claims authenticated ownership")
		}
		if channel.Presence != "" && !types.has(channel.Presence) {
			return invalid("missing presence payload")
		}
		if err := channel.Replay.Validate(); err != nil {
			return err
		}
		if len(channel.Events) > websocket.MaxChannelEvents {
			return invalid("too many channel events")
		}
		events, eventNames := make(map[websocket.EventID]bool), make(map[string]bool)
		for _, event := range channel.Events {
			name, err := clientName(string(event.ID))
			if err != nil {
				return err
			}
			if event.Name != name || events[event.ID] || eventNames[name] || !types.has(event.Payload) {
				return invalid("invalid event or conflicting event name")
			}
			events[event.ID], eventNames[name] = true, true
			if event.Direction != websocket.ClientToServer && event.Direction != websocket.ServerToClient || event.AcceptedAcknowledgement && event.Direction != websocket.ClientToServer {
				return invalid("invalid event direction or acknowledgement")
			}
			if event.Dynamic != (types[event.Payload].Kind == contract.DynamicKind) {
				return invalid("dynamic event disagrees with its schema")
			}
		}
		slices.SortFunc(channel.Events, func(a, b Event) int { return cmp.Compare(a.ID, b.ID) })
	}
	slices.SortFunc(realtime.Channels, func(a, b Channel) int { return cmp.Compare(a.ID, b.ID) })
	return nil
}
