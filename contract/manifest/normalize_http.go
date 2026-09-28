package manifest

import (
	"cmp"
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/contract"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/internal/httppath"
	"github.com/weiloon1234/Foundry-Go/internal/httpquery"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
)

type typeIndex map[contract.TypeID]contract.Type

func (types typeIndex) has(id contract.TypeID) bool { _, ok := types[id]; return id != "" && ok }

func normalizeHTTP(d *Document, types typeIndex) error {
	if d.HTTP == nil {
		d.HTTP = []Operation{}
	}
	if len(d.HTTP)+len(d.RawRoutes) > MaxOperations || len(d.Errors) > MaxOperations {
		return invalid("HTTP catalogue is too large")
	}
	builtins := make(map[foundryhttp.ErrorCode]foundryhttp.ErrorDefinition)
	for _, definition := range foundryhttp.ErrorDefinitions() {
		builtins[definition.Code] = definition
	}
	errors := make(map[foundryhttp.ErrorCode]bool)
	for _, definition := range d.Errors {
		if errors[definition.Code] {
			return invalid("duplicate error code")
		}
		errors[definition.Code] = true
		if builtin, found := builtins[definition.Code]; found {
			if builtin != definition {
				return invalid("builtin error definition changed")
			}
		} else if err := foundryhttp.DefineError(definition.Code, definition.Status, definition.Message).Validate(); err != nil {
			return err
		}
	}
	for code := range builtins {
		if !errors[code] {
			return invalid("builtin error definition is missing")
		}
	}
	slices.SortFunc(d.Errors, func(a, b foundryhttp.ErrorDefinition) int { return cmp.Compare(a.Code, b.Code) })
	expectedError, err := foundryhttp.ErrorResponseJSON().Description()
	if err != nil {
		return err
	}
	if d.ErrorType != expectedError.Root {
		return invalid("HTTP error envelope identity changed")
	}
	for _, expected := range expectedError.Types {
		if !reflect.DeepEqual(types[expected.ID], expected) {
			return invalid("HTTP error envelope shape changed")
		}
	}
	ids, names := make(map[foundryhttp.RouteID]bool), make(map[string]bool)
	for _, route := range d.RawRoutes {
		if !route.Raw || ids[route.ID] {
			return invalid("invalid or duplicate raw route")
		}
		if err := validateRoute(route); err != nil {
			return err
		}
		ids[route.ID] = true
	}
	for index := range d.HTTP {
		op := &d.HTTP[index]
		if op.Path == nil {
			op.Path = []Parameter{}
		}
		if op.Query == nil {
			op.Query = []Parameter{}
		}
		if err := validateRoute(op.Route); err != nil {
			return err
		}
		name, err := clientName(string(op.Route.ID))
		if err != nil {
			return err
		}
		if op.Route.Raw || ids[op.Route.ID] || names[name] || op.Name != name {
			return invalid("duplicate route or conflicting client name")
		}
		ids[op.Route.ID], names[name] = true, true
		if op.Status < 200 || op.Status > 299 || op.Limits.Validate() != nil {
			return invalid("invalid endpoint status or limits")
		}
		if op.Route.Method == foundryhttp.TRACE || (op.Route.Method == foundryhttp.GET || op.Route.Method == foundryhttp.HEAD) && op.Body != nil {
			return invalid("typed endpoint method disagrees with its payload")
		}
		segments, _ := httppath.Parse(op.Route.Path)
		pathIndex := 0
		for _, segment := range segments {
			if segment.Name == "" {
				continue
			}
			if pathIndex >= len(op.Path) {
				return invalid("missing path parameter")
			}
			p := op.Path[pathIndex]
			if p.Name != segment.Name || p.CatchAll != segment.Tail || !p.Required || p.Repeated || p.DefaultURL.IsSet() {
				return invalid("path parameter disagrees with route pattern")
			}
			if err := types.parameter(p); err != nil {
				return err
			}
			pathIndex++
		}
		if pathIndex != len(op.Path) {
			return invalid("unused path parameter")
		}
		seen := make(map[string]bool)
		for _, p := range op.Query {
			if seen[p.Name] || p.CatchAll || !httpquery.ValidName(p.Name) {
				return invalid("invalid or duplicate query parameter")
			}
			seen[p.Name] = true
			if err := types.parameter(p); err != nil {
				return err
			}
		}
		if op.Body != nil && op.Body.MediaType == "application/x-www-form-urlencoded" {
			if err := op.Limits.Form.Validate(); err != nil {
				return err
			}
		}
		if err := types.payload(op.Body, true); err != nil {
			return err
		}
		if err := types.payload(op.Response, false); err != nil {
			return err
		}
		var transferBytes int64
		if op.Response != nil && op.Response.File != nil {
			transferBytes, err = op.Response.File.TransferBytes(op.Limits.Files)
			if err != nil {
				return err
			}
		}
		if op.FileTransferBytes != transferBytes {
			return invalid("file transfer bound disagrees with its declared limits")
		}
		if op.Response != nil && (op.Status == 204 || op.Status == 205) {
			return invalid("bodyless response status has a payload")
		}
		if op.Validation != nil {
			rule, err := op.Validation.Normalize()
			if err != nil {
				return err
			}
			op.Validation = &rule
		}
		codes := make(map[foundryhttp.ErrorCode]bool)
		for _, code := range op.Errors {
			if !errors[code] || codes[code] {
				return invalid("invalid endpoint error reference")
			}
			codes[code] = true
		}
		for code := range builtins {
			if !codes[code] {
				return invalid("endpoint omits a builtin failure")
			}
		}
		if policy := op.Idempotency; policy != nil {
			if err := policy.Validate(); err != nil {
				return err
			}
			if !slices.Contains([]foundryhttp.Method{foundryhttp.POST, foundryhttp.PUT, foundryhttp.PATCH, foundryhttp.DELETE}, op.Route.Method) || op.Body != nil && op.Body.MediaType != "application/json" && op.Body.MediaType != "application/x-www-form-urlencoded" || op.Response != nil && (op.Response.File != nil || op.Response.MediaType != "application/json") {
				return invalid("idempotency has an incompatible transport")
			}
			for _, declaration := range []foundryhttp.ErrorDeclaration{foundryhttp.IdempotencyBadKey, foundryhttp.IdempotencyMismatch, foundryhttp.IdempotencyInProgress, foundryhttp.IdempotencyCapacity, foundryhttp.IdempotencyUnavailable} {
				expected, _ := declaration.Description()
				if !codes[expected.Code] {
					return invalid("idempotency omits a declared outcome")
				}
				for _, actual := range d.Errors {
					if actual.Code == expected.Code && actual != expected {
						return invalid("idempotency outcome changed")
					}
				}
			}
		}
		slices.Sort(op.Errors)
	}
	slices.SortFunc(d.RawRoutes, func(a, b foundryhttp.RouteInfo) int { return cmp.Compare(a.ID, b.ID) })
	slices.SortFunc(d.HTTP, func(a, b Operation) int { return cmp.Compare(a.Route.ID, b.Route.ID) })
	return nil
}

func validateRoute(route foundryhttp.RouteInfo) error {
	if route.SignedURL != nil {
		if err := route.SignedURL.Validate(); err != nil {
			return err
		}
	}
	if !identifier.Semantic(string(route.ID)) || len(route.Path) > 16384 {
		return invalid("invalid route identity or path")
	}
	segments, err := httppath.Parse(route.Path)
	if err != nil {
		return err
	}
	if !slices.Contains([]foundryhttp.Method{foundryhttp.GET, foundryhttp.HEAD, foundryhttp.POST, foundryhttp.PUT, foundryhttp.PATCH, foundryhttp.DELETE, foundryhttp.OPTIONS, foundryhttp.TRACE}, route.Method) {
		return invalid("unsupported route method")
	}
	var parameters []string
	for _, segment := range segments {
		if segment.Name != "" {
			parameters = append(parameters, segment.Name)
		}
	}
	if !slices.Equal(parameters, route.Parameters) {
		return invalid("route parameter names disagree with pattern")
	}
	if route.Access != foundryhttp.Public && route.Access != foundryhttp.Guarded {
		return invalid("route access must be explicit")
	}
	if route.Access == foundryhttp.Guarded && (route.Authentication == nil || route.Authentication.Optional) {
		return invalid("guarded route has no required authentication")
	}
	if a := route.Authentication; a != nil {
		if err := a.Credential.Validate(); err != nil {
			return err
		}
		if !identifier.Semantic(string(a.Guard)) || !identifier.Semantic(string(a.Provider)) {
			return invalid("invalid authentication identity")
		}
		for _, scope := range a.RequiredScopes {
			if !identifier.Semantic(string(scope)) {
				return invalid("invalid scope identity")
			}
		}
		for _, permission := range a.RequiredPermissions {
			if !identifier.Semantic(string(permission)) {
				return invalid("invalid permission identity")
			}
		}
	}
	return nil
}

func (types typeIndex) parameter(p Parameter) error {
	if p.Name == "" || len(p.Name) > 16384 || !utf8.ValidString(p.Name) || strings.ContainsRune(p.Name, 0) || !types.has(p.Type) {
		return invalid("invalid parameter declaration")
	}
	if _, err := (foundryhttp.URLScalarInfo{Value: types[p.Type], Syntax: p.Syntax}).Normalize(); err != nil {
		return err
	}
	if value, present := p.DefaultURL.Get(); present && (p.Required || p.Repeated || len(value) > 16384 || !utf8.ValidString(value)) {
		return invalid("invalid parameter default")
	}
	if text, present := p.DefaultURL.Get(); present && p.Syntax != foundryhttp.CustomURLSyntax {
		typ := types[p.Type]
		data := []byte(text)
		if typ.Kind == contract.StringKind {
			data, _ = json.Marshal(text)
		}
		codec := contract.DefineJSONField[json.RawMessage](contract.Schema{Root: typ.ID, Types: []contract.Type{typ}})
		if _, err := codec.Decode(context.Background(), data, contract.JSONLimits{Bytes: 1 << 17, Depth: 1, Nodes: 2, Steps: 8, Issues: 1}); err != nil {
			return invalid("parameter default disagrees with its scalar")
		}
	}
	return nil
}

func (types typeIndex) payload(payload *Payload, input bool) error {
	if payload == nil {
		return nil
	}
	if payload.MediaType == "application/x-www-form-urlencoded" {
		if !input || payload.File != nil || payload.Type != "" || len(payload.Parts) != 0 {
			return invalid("invalid URL-encoded form contract")
		}
		seen := make(map[string]bool)
		for _, field := range payload.Fields {
			if seen[field.Name] || field.CatchAll || !httpquery.ValidName(field.Name) {
				return invalid("invalid or duplicate form field")
			}
			seen[field.Name] = true
			if err := types.parameter(field); err != nil {
				return err
			}
		}
		return nil
	}
	if len(payload.Fields) != 0 {
		return invalid("form fields require URL-encoded media")
	}
	if payload.File != nil {
		if input || payload.Type != "" || len(payload.Parts) != 0 || len(payload.File.MediaTypes) == 0 || len(payload.File.MediaTypes) > 16 {
			return invalid("invalid file response")
		}
		for _, media := range payload.File.MediaTypes {
			if err := media.Validate(); err != nil {
				return err
			}
		}
		if payload.MediaType != "" && (len(payload.File.MediaTypes) != 1 || payload.MediaType != string(payload.File.MediaTypes[0])) {
			return invalid("file media declarations disagree")
		}
		return nil
	}
	if payload.MediaType == "application/json" {
		if !types.has(payload.Type) || len(payload.Parts) != 0 {
			return invalid("invalid JSON payload reference")
		}
		return nil
	}
	if payload.MediaType != "multipart/form-data" || !input || payload.Type != "" || len(payload.Parts) == 0 || len(payload.Parts) > 1024 {
		return invalid("invalid payload media contract")
	}
	seen := make(map[string]bool)
	for _, part := range payload.Parts {
		if !httpquery.ValidName(part.Name) || seen[part.Name] || part.CatchAll {
			return invalid("invalid multipart part")
		}
		seen[part.Name] = true
		switch part.Kind {
		case foundryhttp.MultipartText:
			if err := types.parameter(part.Parameter); err != nil {
				return err
			}
		case foundryhttp.MultipartJSON:
			if !types.has(part.Type) || part.Syntax != "" || part.DefaultURL.IsSet() {
				return invalid("invalid JSON multipart part")
			}
		case foundryhttp.MultipartFile:
			if part.Type != "" || part.Syntax != "" || part.DefaultURL.IsSet() {
				return invalid("invalid file multipart part")
			}
		default:
			return invalid("unknown multipart part kind")
		}
	}
	return nil
}
