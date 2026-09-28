// Package typescript renders and publishes pure TypeScript clients from the
// shared manifest. Generated HTTP and realtime transports are injected.
package typescript

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/idempotency"
	"github.com/weiloon1234/Foundry-Go/internal/contractname"
	"github.com/weiloon1234/Foundry-Go/internal/generate"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

//go:embed runtime/*.ts
var runtimeSources embed.FS

const maxOutputBytes = 8 << 20

type renderer struct {
	document manifest.Document
	names    map[contract.TypeID]string
	out      bytes.Buffer
}

// Render returns one deterministic, dependency-free ES2022 TypeScript module.
// Runtime payload numbers never pass through JSON.parse. Wide integers and
// exact JSON numbers have decimal-string client values and exact numeric wire
// tokens; json,string continues to use the declared quoted wire format.
func Render(source *manifest.Manifest) ([]byte, error) {
	document, err := source.Snapshot()
	if err != nil {
		return nil, err
	}
	names, err := contractname.Schemas(document.Types)
	if err != nil {
		return nil, err
	}
	for _, typ := range document.Types {
		if length, set := typ.Length.Get(); set && int64(length) > 9007199254740991 {
			return nil, fault.New(fault.Invalid, "TypeScript array length exceeds its exact index range")
		}
	}
	r := renderer{document: document, names: names}
	fmt.Fprintf(&r.out, "%s\n// Requires ES2022 and DOM transport types; no runtime package imports.\n\n", generate.ArtifactHeader)
	fmt.Fprintf(&r.out, "export const manifestVersion = %d as const;\n", manifest.Version)
	fmt.Fprintf(&r.out, "const runtimePolicy: RuntimePolicy = { maxDepth: %d, decimalDigits: %d, metadataBytes: %d, messageBytes: %d };\n", contract.MaxJSONDepth, decimal.MaxDigits, manifest.MaxBytes, i18n.MaxTextBytes)
	fmt.Fprintf(&r.out, "const idempotencyKeyPattern = %s;\n", quote(idempotency.KeyPattern(idempotency.MaxKeyBytes)))
	defaults, _ := json.Marshal(foundryhttp.DefaultEndpointLimits().Response)
	fmt.Fprintf(&r.out, "const defaultJSONLimits: JSONLimits = Object.freeze(%s);\n", defaults)
	for _, name := range []string{"wire", "formats", "validation_messages", "validation", "http", "realtime", "metadata"} {
		data, err := runtimeSources.ReadFile("runtime/" + name + ".ts")
		if err != nil {
			return nil, err
		}
		r.out.Write(data)
		r.out.WriteByte('\n')
	}
	data, err := source.JSON()
	if err != nil {
		return nil, err
	}
	// A JSON string literal is also a safe TypeScript string literal, including
	// escaped line separators and hostile declaration names. Preserve exact JSON.
	literal, _ := json.Marshal(string(data))
	fmt.Fprintf(&r.out, "\nexport const manifestJSON = %s;\nconst runtimeDocument = loadRuntimeDocument();\nconst contracts = new WireCodec(runtimeDocument.types);\n\n", literal)
	r.types()
	r.http()
	r.realtime()
	if r.out.Len() > maxOutputBytes {
		return nil, fault.New(fault.Invalid, "TypeScript output exceeds its publication byte budget")
	}
	return r.out.Bytes(), nil
}

func quote(value string) string                        { data, _ := json.Marshal(value); return string(data) }
func (r *renderer) typeName(id contract.TypeID) string { return r.names[id] }

func (r *renderer) expression(typ contract.Type) string {
	var result string
	switch typ.Kind {
	case contract.BooleanKind:
		result = "boolean"
	case contract.StringKind:
		result = "string"
		if typ.Format == contract.UUIDFormat {
			result = "Identity<" + quote(string(typ.ID)) + ">"
		}
	case contract.IntegerKind:
		result = "number"
		if typ.Bits > 32 {
			result = "string"
		}
	case contract.NumberKind:
		result = "number"
		if typ.Bits == 0 {
			result = "string"
		}
	case contract.ObjectKind:
		var fields []string
		for _, property := range typ.Properties {
			optional := ""
			if !property.Required {
				optional = "?"
			}
			fields = append(fields, "readonly "+quote(property.Name)+optional+": "+r.typeName(property.Type))
		}
		result = "{ " + strings.Join(fields, "; ") + " }"
	case contract.UnionKind:
		variants := make([]string, 0, len(typ.Variants))
		for _, variant := range typ.Variants {
			variants = append(variants, "({ readonly "+quote(typ.Discriminator)+": "+quote(variant.Tag)+" } & "+r.typeName(variant.Type)+")")
		}
		result = strings.Join(variants, " | ")
	case contract.ArrayKind:
		result = "ReadonlyArray<" + r.typeName(typ.Element) + ">"
		if length, set := typ.Length.Get(); set {
			result += " & { readonly length: " + strconv.Itoa(length) + " }"
		}
	case contract.MapKind:
		key := "string"
		if typ.Key != nil && typ.Key.Value.Format == contract.UUIDFormat {
			key = "Identity<" + quote(string(typ.Key.Value.ID)) + ">"
		}
		if typ.Key != nil && len(typ.Key.Value.Cases) != 0 {
			values := make([]string, 0, len(typ.Key.Value.Cases))
			for _, raw := range typ.Key.Value.Cases {
				text := string(raw)
				if typ.Key.Value.Kind == contract.StringKind {
					_ = json.Unmarshal(raw, &text)
				}
				values = append(values, quote(text))
			}
			key = strings.Join(values, " | ")
		}
		result = "Readonly<Partial<Record<" + key + ", " + r.typeName(typ.Element) + ">>>"
	case contract.AliasKind, contract.QuotedKind:
		result = "Exclude<" + r.typeName(typ.Element) + ", null>"
	case contract.DynamicKind:
		result = "Exclude<JSONValue, null>"
	}
	if len(typ.Cases) != 0 {
		values := make([]string, 0, len(typ.Cases))
		for _, raw := range typ.Cases {
			literal := string(raw)
			if typ.Kind == contract.IntegerKind && typ.Bits > 32 {
				literal = quote(literal)
			}
			values = append(values, literal)
		}
		result = strings.Join(values, " | ")
	}
	if typ.Nullable {
		result = "(" + result + ") | null"
	}
	return result
}

func (r *renderer) types() {
	r.out.WriteString("declare const identity: unique symbol;\nexport type Identity<Owner extends string> = string & { readonly [identity]: Owner };\n\n")
	for _, typ := range r.document.Types {
		fmt.Fprintf(&r.out, "export type %s = %s;\n", r.typeName(typ.ID), r.expression(typ))
	}
	r.out.WriteString("\nexport interface ContractTypes {\n")
	for _, typ := range r.document.Types {
		fmt.Fprintf(&r.out, "  readonly %s: %s;\n", quote(string(typ.ID)), r.typeName(typ.ID))
	}
	r.out.WriteString("}\n\n")
	r.out.WriteString(`/** Decode a raw network value using a declared schema and resource bounds. */
export function decodeContract<K extends keyof ContractTypes>(type: K, input: string | Uint8Array, limits: JSONLimits = defaultJSONLimits): ContractTypes[K] {
  return contracts.decode(type, input, limits) as ContractTypes[K];
}
export function encodeContract<K extends keyof ContractTypes>(type: K, input: ContractTypes[K], limits: JSONLimits = defaultJSONLimits): string {
  return contracts.encode(type, input, limits);
}
/** Validate and own a client value, including applying a declared identity brand. */
export function contractValue<K extends keyof ContractTypes>(type: K, input: unknown, limits: JSONLimits = defaultJSONLimits): ContractTypes[K] {
  return decodeContract(type, contracts.encode(type, input, limits), limits);
}
`)
}

func (r *renderer) parameters(parameters []manifest.Parameter) string {
	fields := make([]string, 0, len(parameters))
	for _, parameter := range parameters {
		optional := ""
		if !parameter.Required {
			optional = "?"
		}
		typ := r.typeName(parameter.Type)
		if parameter.Repeated {
			typ = "ReadonlyArray<" + typ + ">"
		}
		fields = append(fields, "readonly "+quote(parameter.Name)+optional+": "+typ)
	}
	return "{ " + strings.Join(fields, "; ") + " }"
}

func (r *renderer) request(op manifest.Operation) string {
	fields := make([]string, 0, 4)
	if op.Idempotency != nil {
		fields = append(fields, "readonly idempotencyKey: IdempotencyKey")
	}
	if op.Route.SignedURL != nil {
		fields = append(fields, "readonly signedURL: string")
	}
	if len(op.Path) > 0 {
		fields = append(fields, "readonly path: "+r.parameters(op.Path))
	}
	if len(op.Query) > 0 {
		optional := "?"
		for _, parameter := range op.Query {
			if parameter.Required {
				optional = ""
				break
			}
		}
		fields = append(fields, "readonly query"+optional+": "+r.parameters(op.Query))
	}
	if op.Body != nil {
		body := r.typeName(op.Body.Type)
		if op.Body.MediaType == "application/x-www-form-urlencoded" {
			body = r.parameters(op.Body.Fields)
		} else if op.Body.Type == "" {
			parts := make([]string, 0, len(op.Body.Parts))
			for _, part := range op.Body.Parts {
				typ := r.typeName(part.Type)
				if part.Kind == foundryhttp.MultipartFile {
					typ = "Upload"
				}
				if part.Repeated {
					typ = "ReadonlyArray<" + typ + ">"
				}
				optional := ""
				if !part.Required {
					optional = "?"
				}
				parts = append(parts, "readonly "+quote(part.Name)+optional+": "+typ)
			}
			body = "{ " + strings.Join(parts, "; ") + " }"
		}
		fields = append(fields, "readonly body: "+body)
	}
	if len(fields) == 0 {
		return "Readonly<Record<string, never>>"
	}
	return "{ " + strings.Join(fields, "; ") + " }"
}

func (r *renderer) response(op manifest.Operation) string {
	if op.Response != nil && op.Response.File != nil {
		return "FileResult"
	}
	if op.Response == nil || op.Route.Method == foundryhttp.HEAD {
		return "void"
	}
	return r.typeName(op.Response.Type)
}

func (r *renderer) http() {
	fmt.Fprintf(&r.out, "\nexport type ErrorResponse = %s;\nexport interface Operations {\n", r.typeName(r.document.ErrorType))
	for _, op := range r.document.HTTP {
		codes := make([]string, len(op.Errors))
		for i, code := range op.Errors {
			codes[i] = quote(string(code))
		}
		fmt.Fprintf(&r.out, "  readonly %s: { readonly request: %s; readonly response: %s; readonly errorCode: %s };\n", quote(op.Name), r.request(op), r.response(op), strings.Join(codes, " | "))
	}
	r.out.WriteString("}\nexport interface API {\n")
	for _, op := range r.document.HTTP {
		fmt.Fprintf(&r.out, "  %s(request: Operations[%s][\"request\"], options?: CallOptions): Promise<Operations[%s][\"response\"]>;\n", quote(op.Name), quote(op.Name), quote(op.Name))
	}
	r.out.WriteString("}\nexport function createClient(transport: HTTPTransport, options: ClientOptions = {}): API {\n  const engine = createHTTPInvoker(runtimeDocument, transport, options);\n  return Object.freeze({\n")
	for _, op := range r.document.HTTP {
		fmt.Fprintf(&r.out, "    [%s]: (request: Operations[%s][\"request\"], options?: CallOptions) => engine.invoke(%s, request, options) as Promise<Operations[%s][\"response\"]>,\n", quote(op.Name), quote(op.Name), quote(op.Name), quote(op.Name))
	}
	r.out.WriteString("  });\n}\n")
	r.out.WriteString(`export function validateRequest<K extends keyof Operations>(operation: K, input: Operations[K]["request"], options: ClientOptions = {}): ValidationReport {
  return createHTTPInvoker(runtimeDocument, async () => { throw new Error("Validation cannot send requests"); }, options).validate(operation, input);
}
`)
}

func (r *renderer) realtime() {
	if r.document.Realtime == nil {
		return
	}
	fmt.Fprintf(&r.out, "\nexport const realtimeSubprotocol = %s;\n", quote(r.document.Realtime.Protocol.Subprotocol))
	for _, channel := range r.document.Realtime.Channels {
		name := "Channel_" + contractname.Symbol(string(channel.ID))
		presence := "never"
		if channel.Presence != "" {
			presence = r.typeName(channel.Presence)
		}
		fmt.Fprintf(&r.out, "export interface %s {\n  subscribe(options?: SubscribeOptions): Promise<readonly PresenceMember<%s>[]>;\n  unsubscribe(options?: CallOptions): Promise<void>;\n  dispose(): void;\n", name, presence)
		if channel.Presence != "" {
			fmt.Fprintf(&r.out, "  onPresence(handler: (change: PresenceChange<%s>) => void): () => void;\n", presence)
		}
		r.out.WriteString("  readonly on: {\n")
		for _, event := range channel.Events {
			if event.Direction == websocket.ServerToClient {
				fmt.Fprintf(&r.out, "    %s(handler: (payload: %s, info: EventInfo) => void): () => void;\n", quote(event.Name), r.typeName(event.Payload))
			}
		}
		r.out.WriteString("  };\n  readonly publish: {\n")
		for _, event := range channel.Events {
			if event.Direction == websocket.ClientToServer {
				options := "Omit<PublishOptions, \"onAccepted\">"
				if event.AcceptedAcknowledgement {
					options = "PublishOptions"
				}
				fmt.Fprintf(&r.out, "    %s(payload: %s, options?: %s): Promise<void>;\n", quote(event.Name), r.typeName(event.Payload), options)
			}
		}
		r.out.WriteString("  };\n}\n")
	}
	r.out.WriteString("export interface Realtime {\n  close(): void;\n  readonly channels: {\n")
	for _, channel := range r.document.Realtime.Channels {
		optional := "?"
		if channel.OwnedRooms {
			optional = ""
		}
		fmt.Fprintf(&r.out, "    %s(room%s: %s): Channel_%s;\n", quote(channel.Name), optional, r.typeName(channel.Room.Type), contractname.Symbol(string(channel.ID)))
	}
	r.out.WriteString("  };\n}\nexport function createRealtime(transport: RealtimeTransport, options: RealtimeOptions = {}): Realtime {\n  const engine = createRealtimeEngine(runtimeDocument, transport, options);\n  return Object.freeze({ close: () => engine.close(), channels: Object.freeze({\n")
	for _, channel := range r.document.Realtime.Channels {
		optional := "?"
		if channel.OwnedRooms {
			optional = ""
		}
		presence := "never"
		if channel.Presence != "" {
			presence = r.typeName(channel.Presence)
		}
		fmt.Fprintf(&r.out, "    [%s]: (room%s: %s): Channel_%s => {\n      const bound = engine.room(%s, room);\n      return Object.freeze({\n        subscribe: (options?: SubscribeOptions) => bound.subscribe(options) as Promise<readonly PresenceMember<%s>[]>,\n        unsubscribe: (options?: CallOptions) => bound.unsubscribe(options),\n        dispose: () => bound.dispose(),\n", quote(channel.Name), optional, r.typeName(channel.Room.Type), contractname.Symbol(string(channel.ID)), quote(channel.Name), presence)
		if channel.Presence != "" {
			fmt.Fprintf(&r.out, "        onPresence: (handler: (change: PresenceChange<%s>) => void) => bound.onPresence(change => handler(change as PresenceChange<%s>)),\n", presence, presence)
		}
		r.out.WriteString("        on: Object.freeze({\n")
		for _, event := range channel.Events {
			if event.Direction == websocket.ServerToClient {
				fmt.Fprintf(&r.out, "          [%s]: (handler: (payload: %s, info: EventInfo) => void) => bound.on(%s, (payload, info) => handler(payload as %s, info)),\n", quote(event.Name), r.typeName(event.Payload), quote(event.Name), r.typeName(event.Payload))
			}
		}
		r.out.WriteString("        }),\n        publish: Object.freeze({\n")
		for _, event := range channel.Events {
			if event.Direction == websocket.ClientToServer {
				options := "Omit<PublishOptions, \"onAccepted\">"
				if event.AcceptedAcknowledgement {
					options = "PublishOptions"
				}
				fmt.Fprintf(&r.out, "          [%s]: (payload: %s, options?: %s) => bound.publish(%s, payload, options),\n", quote(event.Name), r.typeName(event.Payload), options, quote(event.Name))
			}
		}
		r.out.WriteString("        }),\n      });\n    },\n")
	}
	r.out.WriteString("  }) });\n}\n")
}
