package manifest

import (
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/internal/httppath"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

// Selection names the operations and channels of one client surface, such as
// one portal; the entries' selections are combined. A Routes or Channels entry
// selects the route or channel with exactly that ID and every ID continuing it
// after a dot: "admin" selects admin.login and admin.orders.list, not
// administration.list. A Paths entry is a literal path prefix selecting the
// routes whose path equals it or continues it after a slash: "/api/admin"
// selects /api/admin/orders/{id}, not /api/administration.
type Selection struct {
	Routes   []foundryhttp.RouteID
	Channels []websocket.ChannelID
	Paths    []string
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
	routes, err := namespaces(selection.Routes)
	if err != nil {
		return nil, err
	}
	channels, err := namespaces(selection.Channels)
	if err != nil {
		return nil, err
	}
	paths, err := pathPrefixes(selection.Paths)
	if err != nil {
		return nil, err
	}
	if len(routes.entries)+len(channels.entries)+len(paths.entries) == 0 {
		return nil, invalid("a projection selects no route or channel")
	}
	// Both selectors run so that every entry matching a route is marked used.
	selectsRoute := func(route foundryhttp.RouteInfo) bool {
		byID, byPath := routes.selects(string(route.ID)), paths.selects(route.Path)
		return byID || byPath
	}
	result := Document{Version: document.Version, Roots: document.Roots, ErrorType: document.ErrorType, Errors: document.Errors, Locales: document.Locales, Enums: document.Enums, Permissions: document.Permissions}
	for _, op := range document.HTTP {
		if selectsRoute(op.Route) {
			result.HTTP = append(result.HTTP, op)
		}
	}
	for _, route := range document.RawRoutes {
		if selectsRoute(route) {
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
	for _, entry := range slices.Concat(routes.entries, channels.entries, paths.entries) {
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

type selection struct {
	entries []*selectionEntry
	match   func(entry, candidate string) bool
}

// namespaces selects semantic IDs and their dotted namespaces.
func namespaces[T ~string](ids []T) (selection, error) {
	return selector(ids, identifier.Semantic, func(entry, id string) bool {
		return id == entry || strings.HasPrefix(id, entry+".")
	})
}

// pathPrefixes selects route paths by whole literal segments. A prefix uses the
// route grammar without parameters or a trailing slash; "/" selects every path.
func pathPrefixes(paths []string) (selection, error) {
	return selector(paths, func(path string) bool {
		segments, err := httppath.Parse(path)
		if err != nil || path != "/" && strings.HasSuffix(path, "/") {
			return false
		}
		for _, segment := range segments {
			if segment.Name != "" {
				return false
			}
		}
		return true
	}, func(prefix, path string) bool {
		return prefix == "/" || path == prefix || strings.HasPrefix(path, prefix+"/")
	})
}

func selector[T ~string](ids []T, valid func(string) bool, match func(entry, candidate string) bool) (selection, error) {
	if len(ids) > MaxOperations {
		return selection{}, invalid("too many projection entries")
	}
	result := selection{match: match}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !valid(string(id)) {
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

// selects marks every entry matching candidate.
func (s selection) selects(candidate string) bool {
	found := false
	for _, entry := range s.entries {
		if s.match(entry.id, candidate) {
			entry.used, found = true, true
		}
	}
	return found
}
