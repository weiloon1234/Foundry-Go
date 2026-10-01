// Package openapi renders OpenAPI from the shared validated client manifest.
// It owns no parallel request, response, permission or validation declaration.
package openapi

import (
	"encoding/json"
	"maps"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/internal/contractname"
	"github.com/weiloon1234/Foundry-Go/internal/httppath"
)

const SpecificationVersion = "3.1.1"

// Options describe the consumer API. Servers are optional base URLs, either
// absolute http(s) URLs without credentials, query or fragment, or paths
// relative to the document such as "/api".
type Options struct {
	Title      string
	APIVersion string
	Servers    []Server
}

// Server is one OpenAPI server entry.
type Server struct {
	URL         string
	Description string
}

// MaxServers bounds Options.Servers.
const MaxServers = 16

// Validate checks the required title and version and every server entry.
func (o Options) Validate() error {
	if !validText(o.Title) || !validText(o.APIVersion) {
		return fault.New(fault.Invalid, "OpenAPI requires a title and API version")
	}
	if len(o.Servers) > MaxServers {
		return fault.New(fault.Invalid, "OpenAPI servers exceed MaxServers")
	}
	for _, server := range o.Servers {
		if !server.valid() {
			return fault.New(fault.Invalid, "OpenAPI server requires an http(s) URL or absolute path without credentials, query or fragment")
		}
	}
	return nil
}

func (s Server) valid() bool {
	if s.URL == "" || len(s.URL) > 2048 || !utf8.ValidString(s.URL) || strings.ContainsAny(s.URL, " \t\r\n") {
		return false
	}
	if s.Description != "" && !validText(s.Description) {
		return false
	}
	parsed, err := url.Parse(s.URL)
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || strings.Contains(s.URL, "#") {
		return false
	}
	if parsed.Scheme == "" {
		return parsed.Host == "" && strings.HasPrefix(s.URL, "/")
	}
	return (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.Host != ""
}

type object = map[string]any

type renderer struct {
	document manifest.Document
	names    map[contract.TypeID]string
	types    map[contract.TypeID]contract.Type
	variants map[contract.TypeID][]string
}

// Render returns deterministic JSON with named schemas and operation IDs from
// registered descriptors. Title and APIVersion describe the consumer API and
// are required; they are not inferred from a module path or framework version.
func Render(source *manifest.Manifest, options Options) ([]byte, error) {
	if err := options.Validate(); err != nil {
		return nil, err
	}
	document, err := source.Snapshot()
	if err != nil {
		return nil, err
	}
	names, err := contractname.Schemas(document.Types, nil)
	if err != nil {
		return nil, err
	}
	r := renderer{document: document, names: names, types: make(map[contract.TypeID]contract.Type)}
	for _, typ := range document.Types {
		r.types[typ.ID] = typ
	}
	schemas, paths, security := object{}, object{}, object{}
	tags := make(map[string]bool)
	pathShapes := make(map[string]string)
	r.variants = make(map[contract.TypeID][]string)
	occupied := make(map[string]bool, len(names))
	for _, name := range names {
		occupied[name] = true
	}
	for _, typ := range document.Types {
		for i, variant := range typ.Variants {
			name := names[typ.ID] + "_variant_" + strconv.Itoa(i)
			for occupied[name] {
				name += "_"
			}
			occupied[name] = true
			r.variants[typ.ID] = append(r.variants[typ.ID], name)
			schemas[name] = r.taggedVariant(typ, variant)
		}
	}
	for _, typ := range document.Types {
		schemas[names[typ.ID]] = r.schema(typ)
	}
	for _, op := range document.HTTP {
		path := openAPIPath(op.Route.Path)
		segments, _ := httppath.Parse(op.Route.Path)
		var shape strings.Builder
		for _, segment := range segments {
			shape.WriteByte('/')
			if segment.Name == "" {
				shape.WriteString(url.PathEscape(segment.Literal))
			} else {
				shape.WriteString("{}")
			}
		}
		if previous, exists := pathShapes[shape.String()]; exists && previous != path {
			return nil, fault.New(fault.Conflict, "OpenAPI cannot represent different parameter names at the same path position")
		}
		pathShapes[shape.String()] = path
		item, found := paths[path].(object)
		if !found {
			item = object{}
			paths[path] = item
		}
		method := strings.ToLower(string(op.Route.Method))
		if _, duplicate := item[method]; duplicate {
			return nil, fault.New(fault.Conflict, "OpenAPI path/method collision")
		}
		operation := object{"operationId": string(op.Route.ID), "x-foundry-client-name": op.Name, "x-foundry-access": op.Route.Access, "x-foundry-limits": op.Limits}
		if documentation := op.Route.Documentation; documentation != nil {
			if documentation.Summary != "" {
				operation["summary"] = documentation.Summary
			}
			if documentation.Description != "" {
				operation["description"] = documentation.Description
			}
			if len(documentation.Tags) != 0 {
				operation["tags"] = documentation.Tags
				for _, tag := range documentation.Tags {
					tags[tag] = true
				}
			}
			if documentation.Deprecated {
				operation["deprecated"] = true
			}
		}
		if op.Preparation {
			operation["x-foundry-request-preparation"] = true
		}
		if op.FileTransferBytes != 0 {
			operation["x-foundry-file-transfer-bytes"] = op.FileTransferBytes
		}
		parameters := make([]any, 0, len(op.Path)+len(op.Query))
		for _, p := range op.Path {
			parameters = append(parameters, r.parameter(p, "path"))
		}
		for _, p := range op.Query {
			parameters = append(parameters, r.parameter(p, "query"))
		}
		if policy := op.Idempotency; policy != nil {
			operation["x-foundry-idempotency"] = policy
			parameters = append(parameters, object{"name": policy.Header, "in": "header", "required": true, "schema": object{"type": "string", "minLength": policy.MinKeyBytes, "maxLength": policy.MaxKeyBytes, "pattern": policy.KeyPattern}, "description": "Reuse this key only for the same logical submission and trusted caller scope."})
		}
		if signed := op.Route.SignedURL; signed != nil {
			operation["x-foundry-signed-url"] = signed
			for _, name := range []string{signed.ExpiresParameter, signed.SignatureParameter} {
				parameters = append(parameters, object{"name": name, "in": "query", "required": true, "schema": object{"type": "string"}, "description": "Supplied by the server's signed URL"})
			}
		}
		if len(parameters) != 0 {
			operation["parameters"] = parameters
		}
		if op.Body != nil {
			operation["requestBody"] = object{"required": true, "content": r.content(*op.Body)}
		}
		if op.Validation != nil {
			operation["x-foundry-validation"] = op.Validation
		}
		// Each alternative pairs one credential option with one cookie option; an
		// empty option means the operation also accepts the request without it.
		cookies, credentials := []object{{}}, []object{{}}
		if cookie := op.RefreshCookie; cookie != nil {
			operation["x-foundry-refresh-cookie"] = cookie
			if cookie.Reads {
				name := "RefreshCookie_" + contractname.Symbol(string(cookie.Name))
				security[name] = object{"type": "apiKey", "in": "cookie", "name": string(cookie.Name)}
				cookies = []object{{name: []string{}}}
				if cookie.Optional {
					// A cookie logout also succeeds without the cookie.
					cookies = append(cookies, object{})
				}
			}
		}
		if a := op.Route.Authentication; a != nil {
			operation["x-foundry-authentication"] = a
			name := "Credential_" + contractname.Symbol(string(a.Credential.Source)+"/"+string(a.Credential.Kind)+"/"+a.Credential.Name)
			scheme := object{"type": "http", "scheme": "bearer"}
			if a.Credential.Kind == foundryhttp.CookieCredentialKind {
				scheme = object{"type": "apiKey", "in": "cookie", "name": a.Credential.Name}
			}
			security[name] = scheme
			credentials = []object{{name: []string{}}}
			if a.Optional {
				// Anonymous access still presents a required refresh cookie.
				credentials = append(credentials, object{})
			}
		}
		if len(cookies[0])+len(credentials[0]) > 0 {
			requirements := make([]any, 0, len(cookies)*len(credentials))
			for _, credential := range credentials {
				for _, cookie := range cookies {
					requirement := maps.Clone(credential)
					maps.Copy(requirement, cookie)
					requirements = append(requirements, requirement)
				}
			}
			operation["security"] = requirements
		}
		responses := r.errors(op.Errors)
		success := object{"description": "Successful response"}
		if op.Response != nil && op.Route.Method != foundryhttp.HEAD {
			success["content"] = r.content(*op.Response)
		}
		if policy := op.Idempotency; policy != nil {
			if len(policy.ApplicationHeaders) > 0 {
				headers := object{}
				for _, name := range policy.ApplicationHeaders {
					headers[string(name)] = object{"schema": object{"type": "string", "maxLength": 4096}, "description": "Captured application header, preserved on replay"}
				}
				success["headers"] = headers
			}
			for _, status := range []string{"409", "429", "503"} {
				if response, ok := responses[status].(object); ok {
					response["headers"] = object{"Retry-After": object{"schema": object{"type": "integer", "minimum": 1, "maximum": 300}, "description": "Bounded retry delay for in-progress or unavailable outcomes; reuse the same key"}}
				}
			}
		}
		if cookie := op.RefreshCookie; cookie != nil {
			// Never an example value: the header only names the cookie.
			if cookie.Sets || cookie.Clears {
				success["headers"] = object{"Set-Cookie": object{"schema": object{"type": "string"}, "description": refreshCookieHeader(*cookie)}}
			}
			if response, ok := responses["401"].(object); ok && (cookie.Reads || cookie.Clears) {
				clears := "Clears the " + string(cookie.Name) + " refresh cookie"
				if !cookie.Reads {
					// A guarded logout's authentication 401 precedes its cookie handling.
					clears += " when the handler rejects the request; an authentication failure keeps it"
				}
				response["headers"] = object{"Set-Cookie": object{"schema": object{"type": "string"}, "description": clears}}
			}
		}
		responses[strconv.Itoa(op.Status)] = success
		for _, status := range op.Statuses {
			// Alternative statuses share one declared representation.
			alternative := make(object, len(success))
			for key, value := range success {
				alternative[key] = value
			}
			responses[strconv.Itoa(status)] = alternative
		}
		if op.Redirect {
			// A redirect has no body; its target is the Location header.
			responses[strconv.Itoa(op.Status)] = object{"description": "Redirect to a location on this origin", "headers": object{"Location": object{"required": true, "schema": object{"type": "string", "format": "uri-reference"}, "description": "Relative URL on this origin"}}}
		}
		if op.Response != nil && op.Response.File != nil && op.Response.File.Seekable {
			partial := r.content(*op.Response)
			partial["multipart/byteranges"] = object{"schema": object{"type": "string", "format": "binary"}}
			responses["206"] = object{"description": "Partial representation", "content": partial}
			responses["304"] = object{"description": "Representation has not changed"}
		}
		if op.Route.Method == foundryhttp.HEAD {
			for _, response := range responses {
				delete(response.(object), "content")
			}
		}
		operation["responses"] = responses
		item[method] = operation
	}
	components := object{"schemas": schemas}
	if len(security) != 0 {
		components["securitySchemes"] = security
	}
	result := object{"openapi": SpecificationVersion, "info": object{"title": options.Title, "version": options.APIVersion}, "paths": paths, "components": components, "x-foundry-manifest-version": document.Version}
	if len(options.Servers) != 0 {
		servers := make([]any, 0, len(options.Servers))
		for _, server := range options.Servers {
			entry := object{"url": server.URL}
			if server.Description != "" {
				entry["description"] = server.Description
			}
			servers = append(servers, entry)
		}
		result["servers"] = servers
	}
	if len(tags) != 0 {
		names := make([]string, 0, len(tags))
		for tag := range tags {
			names = append(names, tag)
		}
		sort.Strings(names)
		list := make([]any, 0, len(names))
		for _, name := range names {
			list = append(list, object{"name": name})
		}
		result["tags"] = list
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, err
	}
	if len(data) >= manifest.MaxBytes {
		return nil, fault.New(fault.Invalid, "OpenAPI output exceeds its byte budget")
	}
	return append(data, '\n'), nil
}

func validText(text string) bool {
	return strings.TrimSpace(text) != "" && len(text) <= 16384 && utf8.ValidString(text) && !strings.ContainsRune(text, 0)
}

func openAPIPath(pattern string) string {
	segments, _ := httppath.Parse(pattern)
	parts := make([]string, 0, len(segments))
	for _, segment := range segments {
		if segment.Name != "" {
			parts = append(parts, "{"+segment.Name+"}")
		} else {
			parts = append(parts, url.PathEscape(segment.Literal))
		}
	}
	return "/" + strings.Join(parts, "/")
}

func (r renderer) ref(id contract.TypeID) object {
	return object{"$ref": "#/components/schemas/" + r.names[id]}
}

func (r renderer) parameter(p manifest.Parameter, location string) object {
	schema := r.ref(p.Type)
	if p.Syntax == foundryhttp.CustomURLSyntax {
		schema = object{"type": "string", "x-foundry-value": r.ref(p.Type)}
	}
	// A repeated parameter's hint describes each element, as in the manifest.
	presentationSchema(schema, p.Presentation)
	if p.Repeated {
		schema = object{"type": "array", "items": schema}
		if p.Required {
			schema["minItems"] = 1
		}
	}
	if text, present := p.DefaultURL.Get(); present {
		schema["default"] = r.urlDefault(p, text)
	}
	result := object{"name": p.Name, "in": location, "required": p.Required, "schema": schema, "x-foundry-url-syntax": p.Syntax}
	if location == "query" {
		result["style"], result["explode"] = "form", true
	} else {
		result["style"], result["explode"] = "simple", false
	}
	if p.CatchAll {
		result["x-foundry-catch-all"] = true
	}
	return result
}

func (r renderer) urlDefault(p manifest.Parameter, text string) any {
	if p.Syntax == foundryhttp.CustomURLSyntax {
		return text
	}
	switch r.types[p.Type].Kind {
	case contract.BooleanKind:
		return text == "true"
	case contract.IntegerKind, contract.NumberKind:
		return json.Number(text)
	default:
		return text
	}
}

func (r renderer) content(payload manifest.Payload) object {
	if payload.Raw != nil {
		content := object{}
		for _, media := range payload.Raw.MediaTypes {
			content[string(media)] = object{"schema": object{"type": "string", "format": "binary"}}
		}
		return content
	}
	if payload.File != nil {
		content := object{}
		for _, media := range payload.File.MediaTypes {
			content[string(media)] = object{"schema": object{"type": "string", "format": "binary"}}
		}
		return content
	}
	if payload.MediaType == foundryhttp.EventStreamMediaType {
		// OpenAPI 3.1 has no schema for an event sequence; each event's data
		// is the referenced JSON value.
		return object{payload.MediaType: object{"schema": object{"type": "string"}, "x-foundry-event-data": r.ref(payload.Type)}}
	}
	if payload.Type != "" {
		media := object{"schema": r.ref(payload.Type)}
		if len(payload.Example) != 0 {
			media["example"] = payload.Example
		}
		return object{payload.MediaType: media}
	}
	if payload.MediaType == "application/x-www-form-urlencoded" {
		properties, encodings := object{}, object{}
		required := []string{}
		for _, field := range payload.Fields {
			parameter := r.parameter(field, "query")
			properties[field.Name] = parameter["schema"]
			encodings[field.Name] = object{"style": "form", "explode": true}
			if field.Required {
				required = append(required, field.Name)
			}
		}
		schema := object{"type": "object", "properties": properties, "additionalProperties": false}
		if len(required) != 0 {
			schema["required"] = required
		}
		return object{payload.MediaType: object{"schema": schema, "encoding": encodings}}
	}
	properties, encodings := object{}, object{}
	required := make([]string, 0)
	for _, part := range payload.Parts {
		schema := object{"type": "string", "format": "binary"}
		encoding := object{"style": "form", "explode": true}
		switch part.Kind {
		case foundryhttp.MultipartText:
			schema = r.ref(part.Type)
			if part.Syntax == foundryhttp.CustomURLSyntax {
				schema = object{"type": "string", "x-foundry-value": r.ref(part.Type)}
			}
			encoding["contentType"] = "text/plain"
		case foundryhttp.MultipartJSON:
			schema = r.ref(part.Type)
			encoding["contentType"] = "application/json"
		}
		presentationSchema(schema, part.Presentation)
		if part.Repeated {
			schema = object{"type": "array", "items": schema}
			if part.Required {
				schema["minItems"] = 1
			}
		}
		if text, present := part.DefaultURL.Get(); present {
			schema["default"] = r.urlDefault(part.Parameter, text)
		}
		properties[part.Name], encodings[part.Name] = schema, encoding
		if part.Required {
			required = append(required, part.Name)
		}
	}
	schema := object{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) != 0 {
		schema["required"] = required
	}
	return object{payload.MediaType: object{"schema": schema, "encoding": encodings}}
}

func (r renderer) errors(allowed []foundryhttp.ErrorCode) object {
	set := make(map[foundryhttp.ErrorCode]bool)
	for _, code := range allowed {
		set[code] = true
	}
	groups := make(map[int][]string)
	for _, definition := range r.document.Errors {
		if set[definition.Code] {
			groups[definition.Status] = append(groups[definition.Status], string(definition.Code))
		}
	}
	result := object{}
	for status, codes := range groups {
		sort.Strings(codes)
		schema := object{"allOf": []any{r.ref(r.document.ErrorType), object{"properties": object{"status": object{"const": status}, "error_code": object{"enum": codes}}}}}
		result[strconv.Itoa(status)] = object{"description": strings.Join(codes, ", "), "content": object{"application/json": object{"schema": schema}}}
	}
	return result
}

func refreshCookieHeader(cookie foundryhttp.RefreshCookieInfo) string {
	if cookie.Clears {
		return "Clears the " + string(cookie.Name) + " refresh cookie"
	}
	return "Sets the " + string(cookie.Name) + " refresh cookie (HttpOnly, Secure, SameSite=Strict) for a renewable credential"
}
