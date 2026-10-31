// Package typescript renders and publishes pure TypeScript clients from the
// shared manifest. Generated HTTP and realtime transports are injected.
package typescript

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/enum"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/idempotency"
	"github.com/weiloon1234/Foundry-Go/internal/contractname"
	"github.com/weiloon1234/Foundry-Go/internal/generate"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/websocket"
	"golang.org/x/text/currency"
)

//go:embed runtime/*.ts
var runtimeSources embed.FS

// maxOutputBytes shares the artifact publisher's bound, which admits a module
// embedding the largest accepted manifest.
const maxOutputBytes = generate.MaxArtifactBytes

type renderer struct {
	document manifest.Document
	naming
	// uses records the runtime names an entry module's generated code refers
	// to, so it imports exactly those; nil for the self-contained module.
	uses map[string]bool
	out  bytes.Buffer
}

// naming is computed once from the full manifest and shared by every entry, so
// a schema keeps its name in each surface.
type naming struct {
	names map[contract.TypeID]string
	// received names tolerant variants of server-output schemas whose values can
	// contain enum cases or locale keys; other schemas are received unchanged.
	received map[contract.TypeID]string
	// receivedEnums marks received variants that admit unknown enum values.
	receivedEnums map[contract.TypeID]bool
}

// ref records a runtime name the generated code refers to.
func (r *renderer) ref(name string) string {
	if r.uses != nil {
		r.uses[name] = true
	}
	return name
}

// localeKey is the key type of maps keyed by i18n.LocaleID, taken from the
// contract's own key declaration rather than a second spelling of its name.
var localeKey = sync.OnceValue(func() contract.TypeID {
	info, err := contract.StringJSONKey[i18n.LocaleID]().Description()
	if err != nil {
		return ""
	}
	return info.Value.ID
})

// localeKeyed reports a map keyed by i18n.LocaleID. Requests type those keys as
// the catalog's supported locales; received values keep open string keys.
func localeKeyed(typ contract.Type) bool {
	return typ.Kind == contract.MapKind && typ.Key != nil && len(typ.Key.Value.Cases) == 0 && typ.Key.Value.ID == localeKey()
}

// Render returns one deterministic, dependency-free ES2022 TypeScript module
// with the runtime inlined. Generate publishes the same client as an entry
// module importing shared runtime modules. Runtime payload numbers never pass
// through JSON.parse. Wide integers and exact JSON numbers have decimal-string
// client values and exact numeric wire tokens; json,string continues to use the
// declared quoted wire format.
func Render(source *manifest.Manifest) ([]byte, error) {
	document, err := source.Snapshot()
	if err != nil {
		return nil, err
	}
	names, err := newNaming(document)
	if err != nil {
		return nil, err
	}
	r := renderer{document: document, naming: names}
	fmt.Fprintf(&r.out, "%s\n// Requires ES2022 and DOM transport types; no runtime package imports.\n\n", generate.ArtifactHeader)
	r.out.WriteString(preamble(false))
	texts, err := runtimeTexts()
	if err != nil {
		return nil, err
	}
	for _, name := range slices.Concat(coreSources, []string{realtimeSource}, entrySources) {
		r.out.Write(texts[name].inline)
		r.out.WriteByte('\n')
	}
	if err := r.body(source); err != nil {
		return nil, err
	}
	if r.out.Len() > maxOutputBytes {
		return nil, safeerror.NewFault(fault.Invalid, "TypeScript output exceeds its publication byte budget")
	}
	return r.out.Bytes(), nil
}

// newNaming validates the document's TypeScript constraints and names every
// declared schema and its received variant.
func newNaming(document manifest.Document) (naming, error) {
	reserved, err := reservedNames()
	if err != nil {
		return naming{}, err
	}
	names, err := contractname.Schemas(document.Types, func(name string) bool { return reserved[name] })
	if err != nil {
		return naming{}, err
	}
	for _, typ := range document.Types {
		if length, set := typ.Length.Get(); set && int64(length) > 9007199254740991 {
			return naming{}, safeerror.NewFault(fault.Invalid, "TypeScript array length exceeds its exact index range")
		}
	}
	if localeKey() == "" {
		return naming{}, safeerror.NewFault(fault.Internal, "TypeScript locale key declaration is unavailable")
	}
	received, receivedEnums := receivedNames(document, names, reserved)
	return naming{names: names, received: received, receivedEnums: receivedEnums}, nil
}

// preamble declares the Go-owned runtime constants and shared brands. A runtime
// module exports what its sibling modules and entries import.
func preamble(module bool) string {
	var out strings.Builder
	fmt.Fprintf(&out, "export const manifestVersion = %d as const;\n", manifest.Version)
	fmt.Fprintf(&out, "const runtimePolicy: RuntimePolicy = { maxDepth: %d, decimalDigits: %d, metadataBytes: %d, messageBytes: %d, maxMessages: %d, semanticBytes: %d };\n", contract.MaxJSONDepth, decimal.MaxDigits, manifest.MaxBytes, i18n.MaxTextBytes, i18n.MaxMessages, identifier.MaxSemanticBytes)
	fmt.Fprintf(&out, "const semanticPattern = /* @__PURE__ */ new RegExp(%s);\n", quote(identifier.SemanticPattern))
	fmt.Fprintf(&out, "const idempotencyKeyPattern = %s;\n", quote(idempotency.KeyPattern(idempotency.MaxKeyBytes)))
	defaults, _ := json.Marshal(foundryhttp.DefaultEndpointLimits().Response)
	fmt.Fprintf(&out, "const defaultJSONLimits: JSONLimits = /* @__PURE__ */ Object.freeze(%s);\n", defaults)
	kinds := make([]string, 0, len(contract.PresentationKinds()))
	for _, kind := range contract.PresentationKinds() {
		kinds = append(kinds, quote(string(kind)))
	}
	fmt.Fprintf(&out, "/** Presentation kinds, emitted from the Go contract's closed set. */\nexport type PresentationKind = %s;\n", strings.Join(kinds, " | "))
	out.WriteString("declare const identity: unique symbol;\nexport type Identity<Owner extends string> = string & { readonly [identity]: Owner };\n")
	if module {
		out.WriteString("export declare const localeKeyed: unique symbol;\nexport { runtimePolicy, defaultJSONLimits };\n")
	} else {
		out.WriteString("declare const localeKeyed: unique symbol;\n")
	}
	return out.String()
}

// body writes the entry's embedded manifest, types, HTTP client, descriptors
// and realtime client. A JSON string literal is also a safe TypeScript string
// literal, including escaped line separators and hostile declaration names.
// Compact JSON preserves the exact document; a string keeps the compiler's
// work independent of manifest size, and the runtime parses it lazily on first
// use rather than at import.
func (r *renderer) body(source *manifest.Manifest) error {
	data, err := source.JSON()
	if err != nil {
		return err
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, data); err != nil {
		return safeerror.WrapFault(fault.Internal, "manifest JSON is not compactable", err)
	}
	literal, _ := json.Marshal(compact.String())
	fmt.Fprintf(&r.out, "\nexport const manifestJSON = %s;\nlet runtimeDocumentCache: %s | undefined;\nfunction runtimeDocument(): RuntimeDocument { return runtimeDocumentCache ??= loadRuntimeDocument(); }\nlet contractsCache: %s | undefined;\nfunction contracts(): WireCodec { return contractsCache ??= new WireCodec(runtimeDocument().types); }\n\n", literal, r.ref("RuntimeDocument"), r.ref("WireCodec"))
	if err := r.types(); err != nil {
		return err
	}
	if err := r.http(); err != nil {
		return err
	}
	r.descriptors()
	if err := r.tables(); err != nil {
		return err
	}
	r.messages()
	r.frontendMessages()
	if err := r.publicConfig(); err != nil {
		return err
	}
	r.realtime()
	return nil
}

// messages types each declared catalog message's arguments from its parameter
// kinds and formats it with the shared renderer, taking the plural parameter
// from the declaration. Frontend-only keys use the runtime's formatText.
func (r *renderer) messages() {
	r.out.WriteString("\n/** Arguments of each declared catalog message; numbers may also be exact decimal strings. */\nexport interface CatalogMessageArguments {\n")
	if r.document.Locales != nil {
		for _, definition := range r.document.Locales.Messages {
			fields := make([]string, 0, len(definition.Parameters))
			for _, parameter := range definition.Parameters {
				typ := "string"
				switch parameter.Kind {
				case i18n.NumberParameter:
					typ = "number | string"
				case i18n.BooleanParameter:
					typ = "boolean"
				}
				fields = append(fields, "readonly "+quote(parameter.Name)+": "+typ)
			}
			arguments := "Readonly<Record<string, never>>"
			if len(fields) != 0 {
				arguments = "{ " + strings.Join(fields, "; ") + " }"
			}
			fmt.Fprintf(&r.out, "  readonly %s: %s;\n", quote(string(definition.Key)), arguments)
		}
	}
	fmt.Fprintf(&r.out, `}
export type CatalogMessageKey = keyof CatalogMessageArguments;
let catalogDefinitionsCache: ReadonlyMap<string, { readonly plural?: string; readonly plural_kind?: "cardinal" | "ordinal" }> | undefined;
function catalogDefinitions(): ReadonlyMap<string, { readonly plural?: string; readonly plural_kind?: "cardinal" | "ordinal" }> {
  return catalogDefinitionsCache ??= new Map((runtimeDocument().locales?.messages ?? []).map(definition => [definition.key, definition]));
}
/** Format a declared catalog message with typed arguments; its plural parameter comes from its declaration. Missing or unrenderable text returns the key. Use formatText for keys only a frontend catalog declares. */
export function formatMessage<K extends CatalogMessageKey>(messages: %s | undefined, key: K, ...args: {} extends CatalogMessageArguments[K] ? [args?: CatalogMessageArguments[K]] : [args: CatalogMessageArguments[K]]): string {
  const definition = catalogDefinitions().get(key);
  return %s(messages, key, (args[0] ?? {}) as Readonly<Record<string, string | number | boolean>>, { plural: definition?.plural, pluralKind: definition?.plural_kind });
}
`, r.ref("ValidationMessages"), r.ref("formatText"))
}

// frontendMessages types each declared frontend message's arguments from its
// parameter kinds and formats it with the shared renderer, taking the plural
// parameter from the declaration.
func (r *renderer) frontendMessages() {
	if r.document.Locales == nil || len(r.document.Locales.FrontendMessages) == 0 {
		return
	}
	r.out.WriteString("\n/** Arguments of each declared frontend message; numbers may also be exact decimal strings. */\nexport interface FrontendMessageArguments {\n")
	for _, definition := range r.document.Locales.FrontendMessages {
		fields := make([]string, 0, len(definition.Parameters))
		for _, parameter := range definition.Parameters {
			typ := "string"
			switch parameter.Kind {
			case i18n.NumberParameter:
				typ = "number | string"
			case i18n.BooleanParameter:
				typ = "boolean"
			}
			fields = append(fields, "readonly "+quote(parameter.Name)+": "+typ)
		}
		arguments := "Readonly<Record<string, never>>"
		if len(fields) != 0 {
			arguments = "{ " + strings.Join(fields, "; ") + " }"
		}
		fmt.Fprintf(&r.out, "  readonly %s: %s;\n", quote(string(definition.Key)), arguments)
	}
	fmt.Fprintf(&r.out, `}
export type FrontendMessageKey = keyof FrontendMessageArguments;
let frontendDefinitionsCache: ReadonlyMap<string, { readonly plural?: string; readonly plural_kind?: "cardinal" | "ordinal" }> | undefined;
function frontendDefinitions(): ReadonlyMap<string, { readonly plural?: string; readonly plural_kind?: "cardinal" | "ordinal" }> {
  return frontendDefinitionsCache ??= new Map((runtimeDocument().locales?.frontend_messages ?? []).map(definition => [definition.key, definition]));
}
/** Format a declared frontend message with typed arguments; its plural parameter comes from its declaration. Missing or unrenderable text returns the key. */
export function formatFrontendMessage<K extends FrontendMessageKey>(messages: %s | undefined, key: K, ...args: {} extends FrontendMessageArguments[K] ? [args?: FrontendMessageArguments[K]] : [args: FrontendMessageArguments[K]]): string {
  const definition = frontendDefinitions().get(key);
  return %s(messages, key, (args[0] ?? {}) as Readonly<Record<string, string | number | boolean>>, { plural: definition?.plural, pluralKind: definition?.plural_kind });
}
`, r.ref("ValidationMessages"), r.ref("formatText"))
}

// publicConfig types the public configuration schema, validator, and decoder.
func (r *renderer) publicConfig() error {
	if len(r.document.PublicConfig) == 0 {
		return nil
	}
	for _, setting := range r.document.PublicConfig {
		if !r.validPublicFallback(setting) {
			return safeerror.NewFault(fault.Invalid, fmt.Sprintf("public config %q requires a valid fallback for its declared type", setting.Env))
		}
	}

	r.out.WriteString(`
export interface PublicEnvEntry {
  /** What the value controls. Public: bundled into browser code and quoted in guard errors. */
  readonly description: string;
  /** Used when the key is absent or empty. */
  readonly fallback: string;
  readonly isValid: (value: string) => boolean;
}

export const publicEnvSchema = {
`)
	for _, setting := range r.document.PublicConfig {
		validator := "(value: string) => true"
		switch setting.Kind {
		case "locale":
			validator = r.ref("isLocale")
			if r.document.Locales != nil {
				locales := make([]string, len(r.document.Locales.Supported))
				for i, locale := range r.document.Locales.Supported {
					locales[i] = quote(string(locale))
				}
				validator = fmt.Sprintf(`(value: string) => %s(value) && ([%s] as readonly string[]).includes(value)`, r.ref("isLocale"), strings.Join(locales, ", "))
			}
		case "currency":
			validator = r.ref("isCurrency")
		case "time_zone":
			validator = r.ref("isTimeZone")
		case "number":
			validator = "(value: string) => Number.isFinite(Number(value))"
		case "boolean":
			validator = `(value: string) => value === "true" || value === "false" || value === "1" || value === "0"`
		case "enum":
			cases := make([]string, len(setting.EnumCases))
			for i, c := range setting.EnumCases {
				cases[i] = quote(c)
			}
			validator = fmt.Sprintf(`(value: string) => ([%s] as readonly string[]).includes(value)`, strings.Join(cases, ", "))
		default:
			if setting.MaxLength > 0 {
				validator = fmt.Sprintf("(value: string) => value.trim().length <= %d", setting.MaxLength)
			}
		}
		fmt.Fprintf(&r.out, "  %s: {\n    description: %s,\n    fallback: %s,\n    isValid: %s,\n  },\n",
			setting.Env, quote(setting.Description), quote(setting.Fallback), validator)
	}
	fmt.Fprintf(&r.out, `} as const satisfies Record<string, PublicEnvEntry>;

export type PublicEnvKey = keyof typeof publicEnvSchema;

export function isPublicEnvKey(key: string): key is PublicEnvKey {
  return Object.hasOwn(publicEnvSchema, key);
}

/**
 * Checks everything the build tool inlines: one problem per undeclared or invalid key.
 * Messages name keys only; values are never echoed because an undeclared key may hold a secret.
 */
export function findPublicEnvProblems(env: Record<string, unknown>, prefix: string): string[] {
  const problems: string[] = [];
  for (const [key, value] of Object.entries(env)) {
    if (prefix !== "" && !key.startsWith(prefix)) continue;
    if (!isPublicEnvKey(key)) {
      problems.push(key + " is not a declared public key.");
    } else if (!isValidPublicValue(key, value)) {
      problems.push(key + " is invalid: " + publicEnvSchema[key].description);
    }
  }
  return problems;
}

/** Blank values are valid: readers use the key's fallback. */
export function isValidPublicValue(key: PublicEnvKey, value: unknown): boolean {
  if (typeof value !== "string") return false;
  return value.trim() === "" || publicEnvSchema[key].isValid(value.trim());
}

/** Validated public configuration read from environment variables. */
export interface PublicEnv {
`)
	for _, setting := range r.document.PublicConfig {
		propType := "string"
		switch setting.Kind {
		case "number":
			propType = "number"
		case "boolean":
			propType = "boolean"
		case "locale":
			if r.document.Locales != nil {
				propType = "Locale"
			}
		case "enum":
			if len(setting.EnumCases) > 0 {
				cases := make([]string, len(setting.EnumCases))
				for i, c := range setting.EnumCases {
					cases[i] = quote(c)
				}
				propType = strings.Join(cases, " | ")
			}
		}
		fmt.Fprintf(&r.out, "  readonly %s: %s;\n", setting.Name, propType)
	}
	fmt.Fprintf(&r.out, `}

/** Decodes public configuration from raw environment variables, applying declared fallbacks. */
export function decodePublicEnv(env: Record<string, string | undefined>): PublicEnv {
  const read = (key: PublicEnvKey): string => {
    const raw = env[key]?.trim();
    const value = raw || publicEnvSchema[key].fallback;
    if (publicEnvSchema[key].isValid(value)) return value;
    throw new Error(key + " is invalid: " + publicEnvSchema[key].description);
  };
  return Object.freeze({
`)
	for _, setting := range r.document.PublicConfig {
		switch setting.Kind {
		case "number":
			fmt.Fprintf(&r.out, "    %s: Number(read(%s)),\n", setting.Name, quote(setting.Env))
		case "boolean":
			fmt.Fprintf(&r.out, "    %s: read(%s) === \"true\" || read(%s) === \"1\",\n", setting.Name, quote(setting.Env), quote(setting.Env))
		case "locale":
			if r.document.Locales != nil {
				fmt.Fprintf(&r.out, "    %s: read(%s) as PublicEnv[%s],\n", setting.Name, quote(setting.Env), quote(setting.Name))
			} else {
				fmt.Fprintf(&r.out, "    %s: read(%s),\n", setting.Name, quote(setting.Env))
			}
		case "enum":
			fmt.Fprintf(&r.out, "    %s: read(%s) as PublicEnv[%s],\n", setting.Name, quote(setting.Env), quote(setting.Name))
		default:
			fmt.Fprintf(&r.out, "    %s: read(%s),\n", setting.Name, quote(setting.Env))
		}
	}
	r.out.WriteString(`  });
}
`)
	return nil
}

// Validate defaults at the same boundary that determines the emitted client
// type. The decoder also validates the chosen value before returning it.
func (r *renderer) validPublicFallback(setting manifest.PublicSetting) bool {
	value := setting.Fallback
	if value != strings.TrimSpace(value) {
		return false
	}
	switch setting.Kind {
	case manifest.PublicEnum:
		return len(setting.EnumCases) > 0 && slices.Contains(setting.EnumCases, value)
	case manifest.PublicLocale:
		if r.document.Locales != nil {
			return slices.Contains(r.document.Locales.Supported, i18n.LocaleID(value))
		}
		_, err := i18n.ParseLocale(value)
		return err == nil
	case manifest.PublicCurrency:
		unit, err := currency.ParseISO(value)
		return err == nil && unit.String() == value
	case manifest.PublicTimeZone:
		if value == "" || value == "Local" {
			return false
		}
		_, err := time.LoadLocation(value)
		return err == nil
	case manifest.PublicBoolean:
		return value == "true" || value == "false" || value == "1" || value == "0"
	case manifest.PublicNumber:
		number, err := strconv.ParseFloat(value, 64)
		return err == nil && !math.IsNaN(number) && !math.IsInf(number, 0)
	default:
		return setting.MaxLength <= 0 || len(utf16.Encode([]rune(value))) <= setting.MaxLength
	}
}

func quote(value string) string                        { data, _ := json.Marshal(value); return string(data) }
func (r *renderer) typeName(id contract.TypeID) string { return r.names[id] }

// receivedName is the client type of a decoded server value.
func (r *renderer) receivedName(id contract.TypeID) string {
	if name, ok := r.received[id]; ok {
		return name
	}
	return r.names[id]
}

// receivedNames selects schemas reachable from responses, errors, server events,
// presence, table rows, notification payloads and explicit schemas whose values
// can contain enum cases or locale-keyed maps. Their tolerant variants admit
// UnknownEnumValue and open locale keys, so a deployed client can receive
// additive values and locales. The second result marks the variants that
// contain enum cases.
func receivedNames(document manifest.Document, names map[contract.TypeID]string, reserved map[string]bool) (map[contract.TypeID]string, map[contract.TypeID]bool) {
	children := make(map[contract.TypeID][]contract.TypeID, len(document.Types))
	parents := make(map[contract.TypeID][]contract.TypeID, len(document.Types))
	for _, typ := range document.Types {
		count := len(typ.Properties) + len(typ.Variants)
		if typ.Element != "" {
			count++
		}
		if count == 0 {
			continue
		}
		refs := make([]contract.TypeID, 0, count)
		for _, property := range typ.Properties {
			refs = append(refs, property.Type)
		}
		for _, variant := range typ.Variants {
			refs = append(refs, variant.Type)
		}
		if typ.Element != "" {
			refs = append(refs, typ.Element)
		}
		children[typ.ID] = refs
		for _, child := range refs {
			parents[child] = append(parents[child], typ.ID)
		}
	}
	// containing marks the types whose values can contain a type matching seed.
	// Walk reverse edges once per seed family instead of rescanning the graph
	// until a fixed point; deep references and cycles take linear work.
	containing := func(seed func(contract.Type) bool) map[contract.TypeID]bool {
		found := make(map[contract.TypeID]bool)
		var queue []contract.TypeID
		for _, typ := range document.Types {
			if seed(typ) {
				found[typ.ID] = true
				queue = append(queue, typ.ID)
			}
		}
		for head := 0; head < len(queue); head++ {
			for _, parent := range parents[queue[head]] {
				if !found[parent] {
					found[parent] = true
					queue = append(queue, parent)
				}
			}
		}
		return found
	}
	enums := containing(func(typ contract.Type) bool { return len(typ.Cases) != 0 })
	locales := containing(localeKeyed)
	var queue []contract.TypeID
	if document.ErrorType != "" {
		queue = append(queue, document.ErrorType)
	}
	for _, op := range document.HTTP {
		if op.Response != nil && op.Response.Type != "" {
			queue = append(queue, op.Response.Type)
		}
	}
	if document.Realtime != nil {
		for _, channel := range document.Realtime.Channels {
			if channel.Presence != "" {
				queue = append(queue, channel.Presence)
			}
			for _, event := range channel.Events {
				if event.Direction == websocket.ServerToClient {
					queue = append(queue, event.Payload)
				}
			}
		}
	}
	// Table rows and notification payloads are server output too, and explicit
	// schemas are what decodeReceived decodes without a route.
	for _, table := range document.Tables {
		queue = append(queue, table.Row)
	}
	for _, notification := range document.Notifications {
		for _, channel := range notification.Channels {
			queue = append(queue, channel.Payload)
		}
	}
	queue = append(queue, document.Roots...)
	reachable := make(map[contract.TypeID]bool)
	for head := 0; head < len(queue); head++ {
		id := queue[head]
		if reachable[id] {
			continue
		}
		reachable[id] = true
		queue = append(queue, children[id]...)
	}
	taken := make(map[string]bool, len(names))
	for _, name := range names {
		taken[name] = true
	}
	result := make(map[contract.TypeID]string)
	for _, typ := range document.Types {
		if !reachable[typ.ID] || !enums[typ.ID] && !locales[typ.ID] {
			continue
		}
		name := names[typ.ID] + "Received"
		for taken[name] || reserved[name] {
			name += "_"
		}
		taken[name] = true
		result[typ.ID] = name
	}
	return result, enums
}

func (r *renderer) expression(typ contract.Type) string { return r.renderExpression(typ, false) }

// renderExpression renders a declared schema, or with received its tolerant
// server-output variant referring to other received names.
func (r *renderer) renderExpression(typ contract.Type, received bool) string {
	typeName := r.typeName
	if received {
		typeName = r.receivedName
	}
	var result string
	switch typ.Kind {
	case contract.BooleanKind:
		result = "boolean"
	case contract.StringKind:
		result = "string"
		if typ.Format == contract.UUIDFormat {
			result = r.ref("Identity") + "<" + quote(string(typ.ID)) + ">"
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
			fields = append(fields, "readonly "+quote(property.Name)+optional+": "+typeName(property.Type))
		}
		result = "{ " + strings.Join(fields, "; ") + " }"
	case contract.UnionKind:
		variants := make([]string, 0, len(typ.Variants))
		for _, variant := range typ.Variants {
			variants = append(variants, "({ readonly "+quote(typ.Discriminator)+": "+quote(variant.Tag)+" } & "+typeName(variant.Type)+")")
		}
		result = strings.Join(variants, " | ")
	case contract.ArrayKind:
		result = "ReadonlyArray<" + typeName(typ.Element) + ">"
		if length, set := typ.Length.Get(); set {
			result += " & { readonly length: " + strconv.Itoa(length) + " }"
		}
	case contract.MapKind:
		if localeKeyed(typ) && !received {
			result = "LocaleMap<" + typeName(typ.Element) + ">"
			break
		}
		key := "string"
		if typ.Key != nil && typ.Key.Value.Format == contract.UUIDFormat {
			key = r.ref("Identity") + "<" + quote(string(typ.Key.Value.ID)) + ">"
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
		result = "Readonly<Partial<Record<" + key + ", " + typeName(typ.Element) + ">>>"
	case contract.AliasKind, contract.QuotedKind:
		result = "Exclude<" + typeName(typ.Element) + ", null>"
	case contract.DynamicKind:
		result = "Exclude<" + r.ref("JSONValue") + ", null>"
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
		if received {
			result += " | " + r.ref("UnknownEnumValue")
		}
	}
	if typ.Nullable {
		result = "(" + result + ") | null"
	}
	return result
}

func (r *renderer) types() error {
	locales := "string"
	if r.document.Locales != nil && len(r.document.Locales.Supported) != 0 {
		values := make([]string, 0, len(r.document.Locales.Supported))
		for _, locale := range r.document.Locales.Supported {
			values = append(values, quote(string(locale)))
		}
		locales = strings.Join(values, " | ")
	}
	fmt.Fprintf(&r.out, "/** The locale catalog's supported locales; any string when none is exported. */\nexport type Locale = %s;\n", locales)
	// A frozen literal of its own: importing it never loads the embedded manifest.
	catalog := "undefined"
	if r.document.Locales != nil {
		supported := make([]string, 0, len(r.document.Locales.Supported))
		for _, locale := range r.document.Locales.Supported {
			supported = append(supported, quote(string(locale)))
		}
		catalog = fmt.Sprintf("/* @__PURE__ */ Object.freeze({ default: %s, supported: /* @__PURE__ */ Object.freeze([%s] as const) })", quote(string(r.document.Locales.Default)), strings.Join(supported, ", "))
	}
	fmt.Fprintf(&r.out, "/** The exported catalog's default and supported locales, without parsing the manifest; undefined when no catalog is exported. */\nexport const catalogLocales: { readonly default: Locale; readonly supported: readonly Locale[] } | undefined = %s;\n", catalog)
	fmt.Fprintf(&r.out, "/** Input keyed by supported locales; descriptors bind its entries with at() and element(). Received values keep string keys. */\nexport type LocaleMap<V> = Readonly<Partial<Record<Locale, V>>> & { readonly [%s]?: never };\n\n", r.ref("localeKeyed"))
	enumsByID := make(map[contract.TypeID]enum.Definition, len(r.document.Enums))
	for _, def := range r.document.Enums {
		enumsByID[contract.TypeID(def.PackagePath+"."+def.Name)] = def
	}
	for _, typ := range r.document.Types {
		typeName := r.typeName(typ.ID)
		fmt.Fprintf(&r.out, "export type %s = %s;\n", typeName, r.expression(typ))
		def, registered := enumsByID[typ.ID]
		if registered || len(typ.Cases) > 0 {
			if !registered {
				for _, value := range typ.Cases {
					def.Cases = append(def.Cases, enum.WireCase{Value: value})
				}
			}
			if topLevelValueNames[typeName] {
				return safeerror.NewFault(fault.Conflict, fmt.Sprintf("generated enum value name %q collides with top-level declaration", typeName))
			}
			r.renderEnumConst(typeName, typ, def)
		}
	}
	for _, typ := range r.document.Types {
		if name, ok := r.received[typ.ID]; ok {
			note := "its locale keys stay open strings"
			if r.receivedEnums[typ.ID] {
				note = "unknown enum values decode as UnknownEnumValue"
			}
			fmt.Fprintf(&r.out, "/** Server output of %s; %s. */\nexport type %s = %s;\n", r.typeName(typ.ID), note, name, r.renderExpression(typ, true))
		}
	}
	r.out.WriteString("\nexport interface ContractTypes {\n")
	for _, typ := range r.document.Types {
		fmt.Fprintf(&r.out, "  readonly %s: %s;\n", quote(string(typ.ID)), r.typeName(typ.ID))
	}
	r.out.WriteString("}\n\n/** Tolerantly decoded server output for each declared schema. */\nexport interface ReceivedContractTypes {\n")
	for _, typ := range r.document.Types {
		fmt.Fprintf(&r.out, "  readonly %s: %s;\n", quote(string(typ.ID)), r.receivedName(typ.ID))
	}
	r.out.WriteString("}\n\n")
	type schemaEntry struct {
		name string
		id   string
	}
	schemaList := make([]schemaEntry, 0, len(r.document.Types))
	for _, typ := range r.document.Types {
		schemaList = append(schemaList, schemaEntry{name: r.typeName(typ.ID), id: string(typ.ID)})
	}
	slices.SortFunc(schemaList, func(a, b schemaEntry) int {
		return strings.Compare(a.name, b.name)
	})
	r.out.WriteString("/** Named schema identity mapping for decodeContract, decodeReceived, encodeContract and schema(). */\nexport const schemas = /* @__PURE__ */ Object.freeze({\n")
	for _, entry := range schemaList {
		keyProp := entry.name
		if !isJSIdentifier(entry.name) {
			keyProp = quote(entry.name)
		}
		fmt.Fprintf(&r.out, "  %s: %s,\n", keyProp, quote(entry.id))
	}
	r.out.WriteString("} as const);\n\n")
	r.ref("JSONLimits")
	r.ref("defaultJSONLimits")
	r.out.WriteString(`/** Decode a raw network value using a declared schema and resource bounds. */
export function decodeContract<K extends keyof ContractTypes>(type: K, input: string | Uint8Array, limits: JSONLimits = defaultJSONLimits): ContractTypes[K] {
  return contracts().decode(type, input, limits) as ContractTypes[K];
}
/** Decode server output tolerantly: unknown properties are ignored and unknown enum values become UnknownEnumValue. */
export function decodeReceived<K extends keyof ReceivedContractTypes>(type: K, input: string | Uint8Array, limits: JSONLimits = defaultJSONLimits): ReceivedContractTypes[K] {
  return contracts().decode(type, input, limits, true) as ReceivedContractTypes[K];
}
export function encodeContract<K extends keyof ContractTypes>(type: K, input: ContractTypes[K], limits: JSONLimits = defaultJSONLimits): string {
  return contracts().encode(type, input, limits);
}
/** Validate and own a client value, including applying a declared identity brand. */
export function contractValue<K extends keyof ContractTypes>(type: K, input: unknown, limits: JSONLimits = defaultJSONLimits): ContractTypes[K] {
  return decodeContract(type, contracts().encode(type, input, limits), limits);
}
`)
	return nil
}

func (r *renderer) renderEnumConst(typeName string, typ contract.Type, def enum.Definition) {
	caseLiterals := make([]string, 0, len(def.Cases))
	labelEntries := make([]struct{ key, labelKey string }, 0, len(def.Cases))
	for _, c := range def.Cases {
		literal := string(c.Value)
		if typ.Kind == contract.IntegerKind && typ.Bits > 32 {
			literal = quote(literal)
		}
		caseLiterals = append(caseLiterals, literal)

		if c.LabelKey != "" {
			var key string
			if typ.Kind == contract.StringKind {
				_ = json.Unmarshal(c.Value, &key)
			} else {
				key = string(c.Value)
			}
			labelEntries = append(labelEntries, struct{ key, labelKey string }{key: key, labelKey: string(c.LabelKey)})
		}
	}
	r.out.WriteString(fmt.Sprintf("export const %s = /* @__PURE__ */ Object.freeze({\n", typeName))
	r.out.WriteString(fmt.Sprintf("  cases: [%s] as const,\n", strings.Join(caseLiterals, ", ")))
	if len(labelEntries) > 0 {
		satisfiesType := fmt.Sprintf("Record<%s, CatalogMessageKey>", typeName)
		if len(labelEntries) < len(def.Cases) {
			satisfiesType = fmt.Sprintf("Partial<Record<%s, CatalogMessageKey>>", typeName)
		}
		r.out.WriteString("  labels: /* @__PURE__ */ Object.freeze({\n")
		for _, entry := range labelEntries {
			keyProp := entry.key
			if typ.Kind == contract.IntegerKind && typ.Bits > 32 {
				keyProp = quote(entry.key)
			} else if !isJSIdentifier(entry.key) {
				keyProp = quote(entry.key)
			}
			fmt.Fprintf(&r.out, "    %s: %s,\n", keyProp, quote(entry.labelKey))
		}
		fmt.Fprintf(&r.out, "  }) satisfies %s,\n", satisfiesType)
	}
	r.out.WriteString("});\n")
}

var topLevelValueNames = map[string]bool{
	"manifestVersion": true, "runtimePolicy": true, "defaultJSONLimits": true,
	"manifestJSON": true, "catalogLocales": true, "createClient": true,
	"validateRequest": true, "operation": true, "schema": true,
	"decodeContract": true, "decodeReceived": true, "encodeContract": true,
	"contractValue": true, "formatMessage": true, "formatFrontendMessage": true, "createRealtime": true,
	"realtimeProtocols": true, "realtimeSubprotocol": true,
	"schemas": true, "ErrorCode": true,
	"API": true, "UnknownEnumValue": true, "JSONNumber": true,
	"ContractError": true, "ResponseContractError": true,
	"HTTPTransportError": true, "AbortError": true, "RealtimeError": true,
	"enumLabel": true, "newIdentity": true, "catalogTranslations": true, "formatText": true,
	"publicEnvSchema": true, "isPublicEnvKey": true, "findPublicEnvProblems": true,
	"isValidPublicValue": true, "decodePublicEnv": true,
	"isLocale": true, "isCurrency": true, "isTimeZone": true,
}

func isJSIdentifier(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if i == 0 {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '_' || r == '$') {
				return false
			}
		} else {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '$') {
				return false
			}
		}
	}
	return true
}

func errorCodeKey(code string) string {
	parts := strings.FieldsFunc(code, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9')
	})
	var b strings.Builder
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		b.WriteString(strings.ToUpper(part[:1]))
		b.WriteString(strings.ToLower(part[1:]))
	}
	res := b.String()
	if res == "" {
		res = "Error"
	} else if res[0] >= '0' && res[0] <= '9' {
		res = "Error_" + res
	}
	return res
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
		fields = append(fields, "readonly idempotencyKey: "+r.ref("IdempotencyKey"))
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
		if raw := op.Body.Raw; raw != nil {
			media := make([]string, len(raw.MediaTypes))
			for i, declared := range raw.MediaTypes {
				media[i] = quote(string(declared))
			}
			// A single declared media type is the default.
			optional := ""
			if len(media) == 1 {
				optional = "?"
			}
			body = "{ readonly data: " + r.ref("RawData") + "; readonly mediaType" + optional + ": " + strings.Join(media, " | ") + " }"
		} else if op.Body.MediaType == "application/x-www-form-urlencoded" {
			body = r.parameters(op.Body.Fields)
		} else if op.Body.Type == "" {
			parts := make([]string, 0, len(op.Body.Parts))
			for _, part := range op.Body.Parts {
				typ := r.typeName(part.Type)
				if part.Kind == foundryhttp.MultipartFile {
					typ = r.ref("Upload")
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
	if op.Redirect {
		return r.ref("RedirectResult") + "<" + strconv.Itoa(op.Status) + ">"
	}
	if op.Response != nil && op.Response.File != nil {
		return r.ref("FileResult")
	}
	if op.Response == nil || op.Route.Method == foundryhttp.HEAD {
		return "void"
	}
	value := r.receivedName(op.Response.Type)
	if op.Response.MediaType == foundryhttp.EventStreamMediaType {
		return r.ref("EventStreamResult") + "<" + value + ">"
	}
	if len(op.Statuses) != 0 {
		statuses := make([]string, len(op.Statuses))
		for i, status := range op.Statuses {
			statuses[i] = strconv.Itoa(status)
		}
		return r.ref("StatusResult") + "<" + strings.Join(statuses, " | ") + ", " + value + ">"
	}
	return value
}

func (r *renderer) http() error {
	for _, name := range []string{"HTTPTransport", "ClientOptions", "CallOptions", "createHTTPInvoker", "ValidationReport"} {
		r.ref(name)
	}
	seen := make(map[string]string, len(r.document.Errors))
	type errEntry struct {
		key  string
		code string
	}
	entries := make([]errEntry, 0, len(r.document.Errors))
	for _, def := range r.document.Errors {
		codeStr := string(def.Code)
		key := errorCodeKey(codeStr)
		if existing, dup := seen[key]; dup {
			return safeerror.NewFault(fault.Conflict, fmt.Sprintf("error codes %q and %q map to the same PascalCase key %q", existing, codeStr, key))
		}
		seen[key] = codeStr
		entries = append(entries, errEntry{key: key, code: codeStr})
	}
	slices.SortFunc(entries, func(a, b errEntry) int {
		return strings.Compare(a.key, b.key)
	})
	r.out.WriteString("\n/** Application and framework error codes defined in the Go manifest. */\nexport const ErrorCode = /* @__PURE__ */ Object.freeze({\n")
	for _, entry := range entries {
		fmt.Fprintf(&r.out, "  %s: %s,\n", entry.key, quote(entry.code))
	}
	r.out.WriteString("} as const);\nexport type ErrorCode = (typeof ErrorCode)[keyof typeof ErrorCode];\n")
	fmt.Fprintf(&r.out, "\nexport type ErrorResponse = %s;\nexport interface Operations {\n", r.receivedName(r.document.ErrorType))
	for _, op := range r.document.HTTP {
		codes := make([]string, len(op.Errors))
		for i, code := range op.Errors {
			codes[i] = quote(string(code))
		}
		fmt.Fprintf(&r.out, "  readonly %s: { readonly request: %s; readonly response: %s; readonly errorCode: %s };\n", quote(op.Name), r.request(op), r.response(op), strings.Join(codes, " | "))
	}
	r.out.WriteString("}\nexport interface API {\n")
	for _, op := range r.document.HTTP {
		r.out.WriteString(documentationComment(op.Route.Documentation))
		fmt.Fprintf(&r.out, "  %s(request: Operations[%s][\"request\"], options?: CallOptions): Promise<Operations[%s][\"response\"]>;\n", quote(op.Name), quote(op.Name), quote(op.Name))
	}
	r.out.WriteString("}\nexport type Client = API;\nexport function createClient(transport: HTTPTransport, options: ClientOptions = {}): API {\n  const engine = createHTTPInvoker(runtimeDocument(), transport, options);\n  return Object.freeze({\n")
	for _, op := range r.document.HTTP {
		fmt.Fprintf(&r.out, "    [%s]: (request: Operations[%s][\"request\"], options?: CallOptions) => engine.invoke(%s, request, options) as Promise<Operations[%s][\"response\"]>,\n", quote(op.Name), quote(op.Name), quote(op.Name), quote(op.Name))
	}
	r.out.WriteString("  });\n}\n")
	r.out.WriteString(`export function validateRequest<K extends keyof Operations>(operation: K, input: Operations[K]["request"], options: ClientOptions = {}): ValidationReport {
  return createHTTPInvoker(runtimeDocument(), async () => { throw new Error("Validation cannot send requests"); }, options).validate(operation, input);
}
`)
	return nil
}

func (r *renderer) realtime() {
	if r.document.Realtime == nil {
		return
	}
	for _, name := range []string{"RealtimeError", "RealtimeTransport", "RealtimeOptions", "SubscribeOptions", "PresenceMember", "CallOptions", "createRealtimeEngine"} {
		r.ref(name)
	}
	fmt.Fprintf(&r.out, "\nexport const realtimeSubprotocol = %s;\n", quote(r.document.Realtime.Protocol.Subprotocol))
	// A browser cannot set Authorization on a WebSocket; a single-use ticket
	// travels as one extra subprotocol entry, never in the URL.
	fmt.Fprintf(&r.out, "/** Sec-WebSocket-Protocol entries for a new connection, with an optional single-use handshake ticket. Fetch a fresh ticket for every connect and reconnect. */\nexport function realtimeProtocols(ticket?: string): string[] {\n  if (ticket === undefined) return [realtimeSubprotocol];\n  if (!/^[!#$%%&'*+.^_`|~0-9A-Za-z-]+$/.test(ticket)) throw new RealtimeError(\"ticket\");\n  return [realtimeSubprotocol, %s + ticket];\n}\n", quote(r.document.Realtime.Protocol.TicketPrefix))
	for _, channel := range r.document.Realtime.Channels {
		name := "Channel_" + contractname.Symbol(string(channel.ID))
		presence := "never"
		if channel.Presence != "" {
			presence = r.receivedName(channel.Presence)
		}
		fmt.Fprintf(&r.out, "export interface %s {\n  subscribe(options?: SubscribeOptions): Promise<readonly PresenceMember<%s>[]>;\n  unsubscribe(options?: CallOptions): Promise<void>;\n  dispose(): void;\n", name, presence)
		if channel.Presence != "" {
			fmt.Fprintf(&r.out, "  onPresence(handler: (change: %s<%s>) => void): () => void;\n", r.ref("PresenceChange"), presence)
		}
		r.out.WriteString("  readonly on: {\n")
		for _, event := range channel.Events {
			if event.Direction == websocket.ServerToClient {
				fmt.Fprintf(&r.out, "    %s(handler: (payload: %s, info: %s) => void): () => void;\n", quote(event.Name), r.receivedName(event.Payload), r.ref("EventInfo"))
			}
		}
		r.out.WriteString("  };\n  readonly publish: {\n")
		for _, event := range channel.Events {
			if event.Direction == websocket.ClientToServer {
				options := "Omit<" + r.ref("PublishOptions") + ", \"onAccepted\">"
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
	r.out.WriteString("  };\n}\nexport function createRealtime(transport: RealtimeTransport, options: RealtimeOptions = {}): Realtime {\n  const engine = createRealtimeEngine(runtimeDocument(), transport, options);\n  return Object.freeze({ close: () => engine.close(), channels: Object.freeze({\n")
	for _, channel := range r.document.Realtime.Channels {
		optional := "?"
		if channel.OwnedRooms {
			optional = ""
		}
		presence := "never"
		if channel.Presence != "" {
			presence = r.receivedName(channel.Presence)
		}
		fmt.Fprintf(&r.out, "    [%s]: (room%s: %s): Channel_%s => {\n      const bound = engine.room(%s, room);\n      return Object.freeze({\n        subscribe: (options?: SubscribeOptions) => bound.subscribe(options) as Promise<readonly PresenceMember<%s>[]>,\n        unsubscribe: (options?: CallOptions) => bound.unsubscribe(options),\n        dispose: () => bound.dispose(),\n", quote(channel.Name), optional, r.typeName(channel.Room.Type), contractname.Symbol(string(channel.ID)), quote(channel.Name), presence)
		if channel.Presence != "" {
			fmt.Fprintf(&r.out, "        onPresence: (handler: (change: PresenceChange<%s>) => void) => bound.onPresence(change => handler(change as PresenceChange<%s>)),\n", presence, presence)
		}
		r.out.WriteString("        on: Object.freeze({\n")
		for _, event := range channel.Events {
			if event.Direction == websocket.ServerToClient {
				fmt.Fprintf(&r.out, "          [%s]: (handler: (payload: %s, info: EventInfo) => void) => bound.on(%s, (payload, info) => handler(payload as %s, info)),\n", quote(event.Name), r.receivedName(event.Payload), quote(event.Name), r.receivedName(event.Payload))
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

// documentationComment renders route documentation as an indented TSDoc block.
// Text cannot close the comment early.
func documentationComment(documentation *foundryhttp.RouteDocumentation) string {
	if documentation == nil {
		return ""
	}
	var lines []string
	if documentation.Summary != "" {
		lines = append(lines, documentation.Summary)
	}
	if documentation.Description != "" {
		if len(lines) != 0 {
			lines = append(lines, "")
		}
		lines = append(lines, strings.Split(documentation.Description, "\n")...)
	}
	if documentation.Deprecated {
		lines = append(lines, "@deprecated")
	}
	if len(lines) == 0 {
		return ""
	}
	var result strings.Builder
	result.WriteString("  /**\n")
	for _, line := range lines {
		line = strings.ReplaceAll(strings.ReplaceAll(line, "*/", "*\\/"), "\t", "  ")
		result.WriteString(strings.TrimRight("   * "+line, " ") + "\n")
	}
	result.WriteString("   */\n")
	return result.String()
}
