package manifest

import (
	"strings"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

// Selection names the operations and channels of one client surface, such as
// one portal. Each entry selects the route or channel with exactly that ID and
// every ID continuing it after a dot: "admin" selects admin.login and
// admin.orders.list, not administration.list.
type Selection struct {
	Routes   []foundryhttp.RouteID
	Channels []websocket.ChannelID
}

// Project returns the manifest restricted to selection: the selected operations,
// raw routes and channels, the types they reach, the tables and notifications
// that belong to them, and the application-wide error definitions, locales,
// enums and permissions. Every entry must select something. The projection is
// validated like any decoded manifest.
func (m *Manifest) Project(selection Selection) (*Manifest, error) {
	document, err := m.Snapshot()
	if err != nil {
		return nil, err
	}
	routes, err := selector(selection.Routes)
	if err != nil {
		return nil, err
	}
	channels, err := selector(selection.Channels)
	if err != nil {
		return nil, err
	}
	if len(routes.entries) == 0 && len(channels.entries) == 0 {
		return nil, invalid("a projection selects no route or channel")
	}
	result := Document{Version: document.Version, Roots: document.Roots, ErrorType: document.ErrorType, Errors: document.Errors, Locales: document.Locales, Enums: document.Enums, Permissions: document.Permissions}
	for _, op := range document.HTTP {
		if routes.selects(string(op.Route.ID)) {
			result.HTTP = append(result.HTTP, op)
		}
	}
	for _, route := range document.RawRoutes {
		if routes.selects(string(route.ID)) {
			result.RawRoutes = append(result.RawRoutes, route)
		}
	}
	selected := make(map[websocket.ChannelID]bool)
	if document.Realtime != nil {
		realtime := Realtime{Protocol: document.Realtime.Protocol, Limits: document.Realtime.Limits}
		for _, channel := range document.Realtime.Channels {
			if channels.selects(string(channel.ID)) {
				realtime.Channels = append(realtime.Channels, channel)
				selected[channel.ID] = true
			}
		}
		if len(realtime.Channels) != 0 {
			result.Realtime = &realtime
		}
	}
	for _, entry := range append(routes.entries, channels.entries...) {
		if !entry.used {
			return nil, fault.New(fault.Missing, "invalid client contract: projection entry "+entry.id+" selects nothing")
		}
	}
	types := make(typeIndex, len(document.Types))
	for _, typ := range document.Types {
		types[typ.ID] = typ
	}
	reached := make(map[contract.TypeID]bool)
	reach := func(ids ...contract.TypeID) {
		queue := append([]contract.TypeID(nil), ids...)
		for len(queue) != 0 {
			id := queue[len(queue)-1]
			queue = queue[:len(queue)-1]
			typ, ok := types[id]
			if !ok || reached[id] {
				continue
			}
			reached[id] = true
			for _, property := range typ.Properties {
				queue = append(queue, property.Type)
			}
			for _, variant := range typ.Variants {
				queue = append(queue, variant.Type)
			}
			if typ.Element != "" {
				queue = append(queue, typ.Element)
			}
			if typ.Key != nil {
				queue = append(queue, typ.Key.Value.ID)
			}
		}
	}
	reach(document.ErrorType)
	reach(document.Roots...)
	for _, op := range result.HTTP {
		for _, parameter := range append(append([]Parameter(nil), op.Path...), op.Query...) {
			reach(parameter.Type)
		}
		for _, payload := range []*Payload{op.Body, op.Response} {
			if payload == nil {
				continue
			}
			reach(payload.Type)
			for _, field := range payload.Fields {
				reach(field.Type)
			}
			for _, part := range payload.Parts {
				reach(part.Type)
			}
		}
	}
	if result.Realtime != nil {
		for _, channel := range result.Realtime.Channels {
			reach(channel.Room.Type, channel.Presence)
			for _, event := range channel.Events {
				reach(event.Payload)
			}
		}
	}
	// Tables belong to the operations that reach their row or request type.
	for _, table := range document.Tables {
		if reached[table.Row] || reached[table.Request] {
			result.Tables = append(result.Tables, table)
		}
	}
	for _, table := range result.Tables {
		reach(table.Row, table.Request)
	}
	// Inbox deliveries are application-wide; realtime deliveries follow their
	// channel. A notification left without a delivery is not part of the surface.
	for _, notification := range document.Notifications {
		kept := notification
		kept.Channels = nil
		for _, channel := range notification.Channels {
			if channel.Realtime == nil || selected[channel.Realtime.Channel] {
				kept.Channels = append(kept.Channels, channel)
			}
		}
		if len(kept.Channels) != 0 {
			result.Notifications = append(result.Notifications, kept)
		}
	}
	for _, notification := range result.Notifications {
		for _, channel := range notification.Channels {
			reach(channel.Payload)
		}
	}
	for _, typ := range document.Types {
		if reached[typ.ID] {
			result.Types = append(result.Types, typ)
		}
	}
	return freeze(result)
}

type selectionEntry struct {
	id   string
	used bool
}

type selection struct{ entries []*selectionEntry }

func selector[T ~string](ids []T) (selection, error) {
	if len(ids) > MaxOperations {
		return selection{}, invalid("too many projection entries")
	}
	var result selection
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !identifier.Semantic(string(id)) {
			return selection{}, invalid("invalid projection entry")
		}
		if seen[string(id)] {
			return selection{}, fault.New(fault.Duplicate, "invalid client contract: repeated projection entry "+string(id))
		}
		seen[string(id)] = true
		result.entries = append(result.entries, &selectionEntry{id: string(id)})
	}
	return result, nil
}

// selects marks every entry naming id or one of its dotted namespaces.
func (s selection) selects(id string) bool {
	found := false
	for _, entry := range s.entries {
		if id == entry.id || strings.HasPrefix(id, entry.id+".") {
			entry.used, found = true, true
		}
	}
	return found
}
