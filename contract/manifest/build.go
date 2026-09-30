package manifest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
)

func invalid(message string) error {
	return fault.New(fault.Invalid, "invalid client contract: "+message)
}

type graph struct {
	types map[contract.TypeID]contract.Type
	wire  map[contract.TypeID][]byte
	err   error
}

func (g *graph) add(schema contract.Schema) contract.TypeID {
	if g.err != nil {
		return ""
	}
	normalized, err := schema.Normalize()
	if err != nil {
		g.err = err
		return ""
	}
	for _, typ := range normalized.Types {
		wire, err := json.Marshal(typ)
		if err != nil {
			g.err = err
			return ""
		}
		if previous, exists := g.wire[typ.ID]; exists {
			if !bytes.Equal(previous, wire) {
				g.err = invalid("conflicting schema identity " + string(typ.ID))
				return ""
			}
			continue
		}
		if len(g.types) >= jsonwire.MaxNodes {
			g.err = invalid("too many schema nodes")
			return ""
		}
		g.types[typ.ID], g.wire[typ.ID] = typ, wire
	}
	return normalized.Root
}

func (g *graph) scalar(scalar *foundryhttp.URLScalarInfo) (contract.TypeID, foundryhttp.URLSyntax) {
	if scalar == nil {
		g.err = invalid("URL codec has no declared metadata")
		return "", ""
	}
	return g.add(contract.Schema{Root: scalar.Value.ID, Types: []contract.Type{scalar.Value}}), scalar.Syntax
}

func (g *graph) parameter(p foundryhttp.QueryParameterInfo) Parameter {
	id, syntax := g.scalar(p.Scalar)
	return Parameter{Presentation: p.Presentation, Name: p.Name, Type: id, Syntax: syntax, Required: p.Required, Repeated: p.Repeated, DefaultURL: p.DefaultURL}
}

func (g *graph) payload(input *foundryhttp.PayloadInfo) *Payload {
	if input == nil {
		return nil
	}
	result := &Payload{MediaType: input.MediaType, File: input.File, Example: slices.Clone(input.Example)}
	if input.Raw != nil {
		result.Raw = &foundryhttp.RawBodyInfo{MediaTypes: slices.Clone(input.Raw.MediaTypes)}
		return result
	}
	switch {
	case input.MediaType == "application/x-www-form-urlencoded":
		for _, field := range input.Form {
			result.Fields = append(result.Fields, g.parameter(field))
		}
	case input.Multipart != nil:
		for _, part := range input.Multipart.Parts {
			p := Parameter{Presentation: part.Presentation, Name: part.Name, Required: part.Required, Repeated: part.Repeated, DefaultURL: part.DefaultURL}
			if part.Kind == foundryhttp.MultipartText {
				p = g.parameter(part.QueryParameterInfo)
			}
			if part.Schema != nil {
				p.Type = g.add(*part.Schema)
			}
			result.Parts = append(result.Parts, Part{Parameter: p, Kind: part.Kind})
		}
	case input.File == nil:
		result.Type = g.add(input.Schema)
	}
	return result
}

// Build snapshots registered metadata without running request handlers,
// notification renderers, validation rules or any external transport.
func Build(ctx context.Context, sources Sources) (*Manifest, error) {
	if ctx == nil {
		return nil, invalid("context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	g := graph{types: make(map[contract.TypeID]contract.Type), wire: make(map[contract.TypeID][]byte)}
	document := Document{Version: Version, Enums: slices.Clone(sources.Enums), Permissions: slices.Clone(sources.Permissions), Errors: foundryhttp.ErrorDefinitions()}
	errorSchema, err := foundryhttp.ErrorResponseJSON().Description()
	if err != nil {
		return nil, err
	}
	document.ErrorType = g.add(errorSchema)
	if len(sources.Schemas) > MaxOperations {
		return nil, invalid("too many explicit schemas")
	}
	for _, schema := range sources.Schemas {
		document.Roots = append(document.Roots, g.add(schema))
	}
	if sources.HTTP != nil {
		document.Errors = sources.HTTP.ErrorDefinitions()
		endpoints, routes := sources.HTTP.Endpoints(), sources.HTTP.Routes()
		if len(routes) > MaxOperations || len(endpoints) > MaxOperations {
			return nil, invalid("too many HTTP routes")
		}
		for _, route := range routes {
			if route.Raw {
				document.RawRoutes = append(document.RawRoutes, route)
			}
		}
		for _, endpoint := range endpoints {
			name, err := clientName(string(endpoint.Route.ID))
			if err != nil {
				return nil, err
			}
			operation := Operation{Route: endpoint.Route, Name: name, Status: endpoint.Status, Statuses: slices.Clone(endpoint.Statuses), Redirect: endpoint.Redirect, Limits: endpoint.Limits, Preparation: endpoint.Preparation, Idempotency: endpoint.Idempotency, Validation: endpoint.Validation, Body: g.payload(endpoint.Body), Response: g.payload(endpoint.Response)}
			if operation.Response != nil && operation.Response.File != nil {
				operation.FileTransferBytes, err = operation.Response.File.TransferBytes(operation.Limits.Files)
				if err != nil {
					return nil, err
				}
			}
			for _, path := range endpoint.Path {
				id, syntax := g.scalar(path.Scalar)
				operation.Path = append(operation.Path, Parameter{Presentation: path.Presentation, Name: path.Name, Type: id, Syntax: syntax, Required: true, CatchAll: path.CatchAll})
			}
			for _, query := range endpoint.Query {
				operation.Query = append(operation.Query, g.parameter(query))
			}
			for _, definition := range foundryhttp.ErrorDefinitions() {
				operation.Errors = append(operation.Errors, definition.Code)
			}
			for _, definition := range endpoint.Errors {
				operation.Errors = append(operation.Errors, definition.Code)
			}
			document.HTTP = append(document.HTTP, operation)
		}
	}
	if source := sources.Realtime; source != nil {
		if len(source.Channels) > MaxOperations {
			return nil, invalid("too many realtime channels")
		}
		document.Realtime = &Realtime{Protocol: source.Protocol, Limits: source.Limits}
		for _, source := range source.Channels {
			name, err := clientName(string(source.ID))
			if err != nil {
				return nil, err
			}
			roomType, roomSyntax := g.scalar(&source.Room)
			channel := Channel{ID: source.ID, Name: name, Room: Parameter{Name: "room", Type: roomType, Syntax: roomSyntax, Required: source.OwnedRooms}, Private: source.Private, OwnedRooms: source.OwnedRooms, Guard: source.Guard, Provider: source.Provider, Replay: source.Replay}
			if source.Presence != nil {
				channel.Presence = g.add(*source.Presence)
			}
			for _, source := range source.Events {
				name, err := clientName(string(source.ID))
				if err != nil {
					return nil, err
				}
				channel.Events = append(channel.Events, Event{ID: source.ID, Name: name, Direction: source.Direction, Dynamic: source.Dynamic, AcceptedAcknowledgement: source.AcceptedAcknowledgement, Payload: g.add(source.Payload)})
			}
			document.Realtime.Channels = append(document.Realtime.Channels, channel)
		}
	}
	if sources.Notifications != nil {
		entries, err := sources.Notifications.ClientDescriptions()
		if err != nil {
			return nil, err
		}
		if len(entries) > MaxOperations {
			return nil, invalid("too many notification contracts")
		}
		for _, source := range entries {
			entry := Notification{Recipient: source.Recipient, Name: source.Name, Version: source.Version}
			for _, channel := range source.Channels {
				entry.Channels = append(entry.Channels, NotificationChannel{ID: channel.ID, Kind: channel.Kind, Payload: g.add(channel.Payload), Realtime: channel.Realtime})
			}
			document.Notifications = append(document.Notifications, entry)
		}
	}
	if sources.Tables != nil {
		entries, err := sources.Tables.Descriptions()
		if err != nil {
			return nil, err
		}
		if len(entries) > MaxOperations {
			return nil, invalid("too many datatable contracts")
		}
		for _, source := range entries {
			document.Tables = append(document.Tables, Table{ID: source.ID, Row: g.add(source.Row), Request: g.add(source.Request), Columns: source.Columns, Filters: source.Filters, DefaultSort: source.DefaultSort, Exports: source.Exports})
		}
	}
	if sources.Catalog != nil {
		locales, err := sources.Catalog.Snapshot(ctx)
		if err != nil {
			return nil, err
		}
		document.Locales = &Locales{Default: locales.Default(), Supported: locales.Locales(), Messages: sources.Catalog.Definitions()}
	}
	if g.err != nil {
		return nil, g.err
	}
	for _, typ := range g.types {
		document.Types = append(document.Types, typ)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return freeze(document)
}

// IDs are semantic source identities. Target names are derived deterministically
// and later checked for collisions; registration order never resolves a clash.
func clientName(id string) (string, error) {
	if !identifier.Semantic(id) {
		return "", invalid("operation requires a semantic ID")
	}
	parts := strings.FieldsFunc(id, func(r rune) bool { return r == '.' || r == '-' || r == '_' })
	if len(parts) == 0 {
		return "", invalid("operation ID has no name")
	}
	name := parts[0]
	for _, part := range parts[1:] {
		name += strings.ToUpper(part[:1]) + part[1:]
	}
	if name[0] >= '0' && name[0] <= '9' {
		name = "operation" + name
	}
	if slices.Contains([]string{"then", "constructor", "prototype", "__proto__"}, name) {
		return "", invalid(fmt.Sprintf("reserved client operation name %q", name))
	}
	return name, nil
}
