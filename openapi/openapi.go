// Package openapi renders OpenAPI from the shared validated client manifest.
// It owns no parallel request, response, permission or validation declaration.
package openapi

import (
	"encoding/json"
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

type Options struct {
	Title      string
	APIVersion string
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
	if !validText(options.Title) || !validText(options.APIVersion) {
		return nil, fault.New(fault.Invalid, "OpenAPI requires a title and API version")
	}
	document, err := source.Snapshot()
	if err != nil {
		return nil, err
	}
	names, err := contractname.Schemas(document.Types)
	if err != nil {
		return nil, err
	}
	r := renderer{document: document, names: names, types: make(map[contract.TypeID]contract.Type)}
	for _, typ := range document.Types {
		r.types[typ.ID] = typ
	}
	schemas, paths, security := object{}, object{}, object{}
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
		if a := op.Route.Authentication; a != nil {
			operation["x-foundry-authentication"] = a
			name := "Credential_" + contractname.Symbol(string(a.Credential.Source)+"/"+string(a.Credential.Kind)+"/"+a.Credential.Name)
			scheme := object{"type": "http", "scheme": "bearer"}
			if a.Credential.Kind == foundryhttp.CookieCredentialKind {
				scheme = object{"type": "apiKey", "in": "cookie", "name": a.Credential.Name}
			}
			security[name] = scheme
			requirements := []any{object{name: []string{}}}
			if a.Optional {
				requirements = append(requirements, object{})
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
		responses[strconv.Itoa(op.Status)] = success
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
	if payload.File != nil {
		content := object{}
		for _, media := range payload.File.MediaTypes {
			content[string(media)] = object{"schema": object{"type": "string", "format": "binary"}}
		}
		return content
	}
	if payload.Type != "" {
		return object{payload.MediaType: object{"schema": r.ref(payload.Type)}}
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
