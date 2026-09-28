package notifications

import (
	"cmp"
	"slices"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

// ClientRealtimeInfo links a notification to its actual registered outgoing
// WebSocket event. The WebSocket registry still owns room/access/replay metadata.
type ClientRealtimeInfo struct {
	Channel websocket.ChannelID `json:"channel"`
	Event   websocket.EventID   `json:"event"`
}

type ClientChannelInfo struct {
	ID       ChannelID           `json:"id"`
	Kind     string              `json:"kind"`
	Payload  contract.Schema     `json:"payload"`
	Realtime *ClientRealtimeInfo `json:"realtime,omitempty"`
}

// ClientNotificationInfo exposes only rendered inbox/realtime DTOs. Send inputs,
// email snapshots and custom transport payloads are not browser contracts.
type ClientNotificationInfo struct {
	Recipient RecipientName       `json:"recipient"`
	Name      Name                `json:"name"`
	Version   Version             `json:"version"`
	Channels  []ClientChannelInfo `json:"channels"`
}

// ClientDescriptions returns deterministic owned metadata from the same typed
// bindings used for delivery, without resolving recipients or running renderers.
// Bindings with no client-facing channel are omitted.
func (r *Registry) ClientDescriptions() ([]ClientNotificationInfo, error) {
	if r == nil {
		return nil, invalid()
	}
	result := make([]ClientNotificationInfo, 0, len(r.entries))
	for key, entry := range r.entries {
		info := ClientNotificationInfo{Recipient: key.recipient, Name: key.name, Version: key.version}
		for _, channel := range entry.channels {
			if channel.client == nil {
				continue
			}
			schema, err := channel.client()
			if err != nil {
				return nil, err
			}
			client := ClientChannelInfo{ID: channel.id, Kind: channel.kind, Payload: schema}
			if channel.realtime != nil {
				copy := *channel.realtime
				client.Realtime = &copy
			}
			info.Channels = append(info.Channels, client)
		}
		if len(info.Channels) == 0 {
			continue
		}
		slices.SortFunc(info.Channels, func(a, b ClientChannelInfo) int { return cmp.Compare(a.ID, b.ID) })
		result = append(result, info)
	}
	slices.SortFunc(result, func(a, b ClientNotificationInfo) int {
		if order := cmp.Compare(a.Recipient, b.Recipient); order != 0 {
			return order
		}
		if order := cmp.Compare(a.Name, b.Name); order != 0 {
			return order
		}
		return cmp.Compare(a.Version, b.Version)
	})
	return result, nil
}
