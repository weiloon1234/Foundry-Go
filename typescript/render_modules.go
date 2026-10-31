package typescript

import (
	"bytes"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/internal/generate"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// Runtime sources by module, in emission order. The core and realtime modules
// are shared by every entry in a directory; the entry sources depend on an
// entry's own operations and embedded manifest, so each entry inlines them.
var (
	coreSources  = []string{"wire", "formats", "validation_messages", "validation", "http"}
	entrySources = []string{"metadata", "descriptors", "forms"}
)

const realtimeSource = "realtime"
const authSource = "auth"

// coreSpecifier is the core module as the runtime sources import it.
const coreSpecifier = `"./runtime.js"`

// moduleLine matches a source's module wiring: its imports from the core and
// its exports of internal names to sibling modules. The self-contained module
// drops these lines; public declarations keep their own export keyword.
var moduleLine = regexp.MustCompile(`^(?:import (?:type )?\{[^}]*\} from "\./runtime\.js";|export (?:type )?\{[^}]*\};)$`)

var importLine = regexp.MustCompile(`^import (type )?\{([^}]*)\} from "\./runtime\.js";$`)

var declaration = regexp.MustCompile(`(?m)^(export\s+)?(declare\s+)?(?:abstract\s+)?(?:async\s+)?(function\*?|class|const|let|interface|type)\s+([A-Za-z_$][\w$]*)`)

func runtimeSource(name string) ([]byte, error) {
	return runtimeSources.ReadFile("runtime/" + name + ".ts")
}

func withoutModuleLines(data []byte) []byte {
	var out bytes.Buffer
	for line := range bytes.Lines(data) {
		if !moduleLine.Match(bytes.TrimRight(line, "\n")) {
			out.Write(line)
		}
	}
	return out.Bytes()
}

type runtimeText struct {
	inline  []byte
	imports []string
}

// Embedded runtime text is immutable and independent of any application.
// Parse its wiring once; entries copy text into their own output buffers.
var runtimeTexts = sync.OnceValues(func() (map[string]runtimeText, error) {
	result := make(map[string]runtimeText)
	for _, name := range slices.Concat(coreSources, []string{realtimeSource}, entrySources) {
		data, err := runtimeSource(name)
		if err != nil {
			return nil, err
		}
		text := runtimeText{inline: withoutModuleLines(data)}
		for line := range bytes.Lines(data) {
			if match := importLine.FindSubmatch(bytes.TrimRight(line, "\n")); match != nil {
				for imported := range strings.SplitSeq(string(match[2]), ",") {
					text.imports = append(text.imports, strings.TrimSpace(imported))
				}
			}
		}
		result[name] = text
	}
	return result, nil
})

// runtimeName describes a top-level runtime declaration: its module, whether it
// exists at run time, and whether entries re-export it as public API.
type runtimeName struct {
	realtime bool
	value    bool
	public   bool
}

// preambleNames are the preamble's declarations (see preamble).
var preambleNames = map[string]runtimeName{
	"manifestVersion":   {value: true, public: true},
	"runtimePolicy":     {value: true},
	"defaultJSONLimits": {value: true},
	"PresentationKind":  {public: true},
	"Identity":          {public: true},
	"localeKeyed":       {},
}

// runtimeNames indexes the core and realtime declarations. An ambient declare
// const has no run-time binding, so it is imported only as a type.
var runtimeNames = sync.OnceValues(func() (map[string]runtimeName, error) {
	result := maps.Clone(preambleNames)
	for _, name := range append(slices.Clone(coreSources), realtimeSource) {
		data, err := runtimeSource(name)
		if err != nil {
			return nil, err
		}
		for _, match := range declaration.FindAllSubmatch(data, -1) {
			kind, ambient := string(match[3]), len(match[2]) != 0
			result[string(match[4])] = runtimeName{realtime: name == realtimeSource, value: !ambient && kind != "interface" && kind != "type", public: len(match[1]) != 0 && !ambient}
		}
	}
	return result, nil
})

func moduleFile(prefix, module string) string {
	if module == "" {
		return prefix + "_runtime_foundry.gen.ts"
	}
	return prefix + "_runtime_" + module + "_foundry.gen.ts"
}

// specifier is the import path of a generated module from its siblings.
func specifier(file string) string {
	return quote("./" + strings.TrimSuffix(file, ".ts") + ".js")
}

const moduleNotice = "// Shared runtime of the generated client entries in this directory. Import an\n// entry module; this module's exports are not a stable API.\n\n"

// coreModule holds the preamble and the core runtime.
func coreModule() ([]byte, error) {
	var out bytes.Buffer
	fmt.Fprintf(&out, "%s\n%s", generate.ArtifactHeader, moduleNotice)
	out.WriteString(preamble(true))
	for _, name := range coreSources {
		data, err := runtimeSource(name)
		if err != nil {
			return nil, err
		}
		out.Write(data)
		out.WriteByte('\n')
	}
	return out.Bytes(), nil
}

// realtimeModule holds the realtime runtime, importing the core.
func realtimeModule(prefix string) ([]byte, error) {
	data, err := runtimeSource(realtimeSource)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	fmt.Fprintf(&out, "%s\n%s", generate.ArtifactHeader, moduleNotice)
	out.Write(bytes.ReplaceAll(data, []byte(coreSpecifier), []byte(specifier(moduleFile(prefix, "")))))
	return out.Bytes(), nil
}

// authModule holds the authentication runtime, importing the core.
func authModule(prefix string) ([]byte, error) {
	data, err := runtimeSource(authSource)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	fmt.Fprintf(&out, "%s\n%s", generate.ArtifactHeader, moduleNotice)
	out.Write(bytes.ReplaceAll(data, []byte(coreSpecifier), []byte(specifier(moduleFile(prefix, "")))))
	return out.Bytes(), nil
}

// entryModule renders one client entry from a full or projected manifest. It
// imports exactly the runtime names its code refers to and re-exports the
// public runtime API; the realtime API is re-exported when exportRealtime.
func entryModule(prefix string, source *manifest.Manifest, document manifest.Document, names naming, exportRealtime bool, extraTail ...[]byte) ([]byte, error) {
	declared, err := runtimeNames()
	if err != nil {
		return nil, err
	}
	r := renderer{document: document, naming: names, uses: make(map[string]bool)}
	texts, err := runtimeTexts()
	if err != nil {
		return nil, err
	}
	for _, name := range entrySources {
		text := texts[name]
		for _, imported := range text.imports {
			r.uses[imported] = true
		}
		r.out.Write(text.inline)
		r.out.WriteByte('\n')
	}
	if err := r.body(source); err != nil {
		return nil, err
	}
	// imports[module][value] lists names; module 0 is the core, 1 realtime.
	var imports, exports [2][2][]string
	for name := range r.uses {
		info, ok := declared[name]
		if !ok {
			return nil, safeerror.NewFault(fault.Internal, "generated client refers to an undeclared runtime name "+name)
		}
		imports[index(info.realtime)][index(info.value)] = append(imports[index(info.realtime)][index(info.value)], name)
	}
	for name, info := range declared {
		if info.public && (exportRealtime || !info.realtime) {
			exports[index(info.realtime)][index(info.value)] = append(exports[index(info.realtime)][index(info.value)], name)
		}
	}
	var out bytes.Buffer
	fmt.Fprintf(&out, "%s\n// Requires ES2022 and DOM transport types; imports only its sibling generated runtime modules.\n\n", generate.ArtifactHeader)
	for module, file := range []string{moduleFile(prefix, ""), moduleFile(prefix, realtimeSource)} {
		for _, kind := range []struct {
			keyword string
			names   []string
		}{{"import", imports[module][1]}, {"import type", imports[module][0]}, {"export", exports[module][1]}, {"export type", exports[module][0]}} {
			if len(kind.names) != 0 {
				slices.Sort(kind.names)
				fmt.Fprintf(&out, "%s { %s } from %s;\n", kind.keyword, strings.Join(kind.names, ", "), specifier(file))
			}
		}
	}
	out.WriteByte('\n')
	out.Write(r.out.Bytes())
	for _, tail := range extraTail {
		if len(tail) != 0 {
			out.Write(tail)
		}
	}
	return out.Bytes(), nil
}

func index(set bool) int {
	if set {
		return 1
	}
	return 0
}

func renderSurfaceAuth(prefix string, surface Surface, fullDoc manifest.Document, view manifest.Document, names naming) ([]byte, error) {
	surfaceAuth := surface.Auth
	if surfaceAuth == nil {
		return nil, nil
	}
	findFull := func(id foundryhttp.RouteID) *manifest.Operation {
		for _, op := range fullDoc.HTTP {
			if op.Route.ID == id {
				return &op
			}
		}
		return nil
	}
	findView := func(id foundryhttp.RouteID) *manifest.Operation {
		for _, op := range view.HTTP {
			if op.Route.ID == id {
				return &op
			}
		}
		return nil
	}

	for _, routeID := range []foundryhttp.RouteID{surfaceAuth.Login, surfaceAuth.Refresh, surfaceAuth.Logout, surfaceAuth.Profile} {
		if findFull(routeID) == nil {
			return nil, safeerror.NewFault(fault.Missing, fmt.Sprintf("surface %q auth route %q is not registered", surface.Name, routeID))
		}
		if findView(routeID) == nil {
			return nil, safeerror.NewFault(fault.Invalid, fmt.Sprintf("surface %q auth references route %q which is not included in the surface", surface.Name, routeID))
		}
	}

	loginOp := findView(surfaceAuth.Login)
	refreshOp := findView(surfaceAuth.Refresh)
	logoutOp := findView(surfaceAuth.Logout)
	profileOp := findView(surfaceAuth.Profile)

	for _, op := range []*manifest.Operation{loginOp, refreshOp, logoutOp, profileOp} {
		unsupplied := len(op.Path) > 0 || op.Idempotency != nil || op.Route.SignedURL != nil || op.Body != nil && op.Route.ID != surfaceAuth.Login
		for _, parameter := range op.Query {
			unsupplied = unsupplied || parameter.Required
		}
		if unsupplied {
			return nil, safeerror.NewFault(fault.Invalid, fmt.Sprintf("surface %q auth route %q requires inputs the session helper cannot supply", surface.Name, op.Route.ID))
		}
	}

	var actualProfileGuard auth.GuardName
	if profileOp.Route.Authentication != nil {
		actualProfileGuard = profileOp.Route.Authentication.Guard
	}
	if actualProfileGuard != surfaceAuth.Guard {
		return nil, safeerror.NewFault(fault.Invalid, fmt.Sprintf("surface %q auth guard %q does not match profile route %q guard %q", surface.Name, surfaceAuth.Guard, surfaceAuth.Profile, actualProfileGuard))
	}

	if loginOp.RefreshCookie == nil || !loginOp.RefreshCookie.Sets || loginOp.RefreshCookie.Reads {
		return nil, safeerror.NewFault(fault.Invalid, fmt.Sprintf("surface %q login route %q must use TokenCookieResponse", surface.Name, surfaceAuth.Login))
	}
	if refreshOp.RefreshCookie == nil || !refreshOp.RefreshCookie.Reads || !refreshOp.RefreshCookie.Sets {
		return nil, safeerror.NewFault(fault.Invalid, fmt.Sprintf("surface %q refresh route %q must use RefreshTokenCookie and TokenCookieResponse", surface.Name, surfaceAuth.Refresh))
	}
	if logoutOp.RefreshCookie == nil || !logoutOp.RefreshCookie.Reads || !logoutOp.RefreshCookie.Clears {
		return nil, safeerror.NewFault(fault.Invalid, fmt.Sprintf("surface %q logout route %q must use RefreshTokenCookieLogout and clear the cookie", surface.Name, surfaceAuth.Logout))
	}

	cookieName := loginOp.RefreshCookie.Name
	for _, other := range []foundryhttp.CookieName{refreshOp.RefreshCookie.Name, logoutOp.RefreshCookie.Name} {
		if cookieName != other {
			return nil, safeerror.NewFault(fault.Invalid, fmt.Sprintf("surface %q auth endpoints must share the same refresh cookie, found %q and %q", surface.Name, cookieName, other))
		}
	}
	if !strings.HasPrefix(string(cookieName), "__Host-") {
		return nil, safeerror.NewFault(fault.Invalid, fmt.Sprintf("surface %q refresh cookie %q requires a __Host- name", surface.Name, cookieName))
	}

	if loginOp.Body == nil || loginOp.Body.Type == "" {
		return nil, safeerror.NewFault(fault.Invalid, fmt.Sprintf("surface %q login route %q requires a request body", surface.Name, surfaceAuth.Login))
	}
	var bodyType *contract.Type
	for _, t := range view.Types {
		if t.ID == loginOp.Body.Type {
			bodyType = &t
			break
		}
	}
	if bodyType == nil || bodyType.Kind != contract.ObjectKind {
		return nil, safeerror.NewFault(fault.Invalid, fmt.Sprintf("surface %q login route %q request body %q must be an object", surface.Name, surfaceAuth.Login, loginOp.Body.Type))
	}

	isPassword := func(p contract.Property) bool {
		return p.Presentation.Kind == contract.PasswordPresentation || p.Name == "password" || strings.HasSuffix(strings.ToLower(p.Name), "password")
	}
	passCount := 0
	var idProp *contract.Property
	for _, p := range bodyType.Properties {
		if isPassword(p) {
			passCount++
		}
		if p.Name == surfaceAuth.Identifier {
			propCopy := p
			idProp = &propCopy
		}
	}
	if passCount == 0 {
		return nil, safeerror.NewFault(fault.Invalid, fmt.Sprintf("surface %q login route %q body %q contains no password field", surface.Name, surfaceAuth.Login, bodyType.ID))
	}
	if passCount > 1 {
		return nil, safeerror.NewFault(fault.Invalid, fmt.Sprintf("surface %q login route %q body %q contains multiple password fields; exactly one is permitted", surface.Name, surfaceAuth.Login, bodyType.ID))
	}
	if idProp == nil {
		return nil, safeerror.NewFault(fault.Invalid, fmt.Sprintf("surface %q auth login route %q body %q: declared identifier %q must exist as a non-secret JSON property", surface.Name, surfaceAuth.Login, bodyType.ID, surfaceAuth.Identifier))
	}
	if isPassword(*idProp) {
		return nil, safeerror.NewFault(fault.Invalid, fmt.Sprintf("surface %q auth login route %q body %q: identifier %q cannot be a secret or password field", surface.Name, surfaceAuth.Login, bodyType.ID, surfaceAuth.Identifier))
	}

	credentialsType := names.names[loginOp.Body.Type]
	profileType := fmt.Sprintf("Operations[%s][\"response\"]", quote(profileOp.Name))

	var b bytes.Buffer
	fmt.Fprintf(&b, "\nimport { createPortalSession as runtimeCreatePortalSession } from %s;\n", specifier(moduleFile(prefix, authSource)))
	fmt.Fprintf(&b, "import type { SurfaceAuthDescriptor, PortalSession, PortalSessionOptions } from %s;\n\n", specifier(moduleFile(prefix, authSource)))
	fmt.Fprintf(&b, "/** Password credentials accepted by this portal's browser login endpoint. */\n")
	fmt.Fprintf(&b, "export type PasswordCredentials = %s;\n\n", credentialsType)
	fmt.Fprintf(&b, "/** The declared login identifier JSON property name for this portal. */\n")
	fmt.Fprintf(&b, "export const signInIdentifier: keyof PasswordCredentials = %s;\n\n", quote(surfaceAuth.Identifier))
	fmt.Fprintf(&b, "/** Surface authentication descriptor binding this portal's operations to the client runtime. */\n")
	fmt.Fprintf(&b, "export const authDescriptor: SurfaceAuthDescriptor<Client, %s, PasswordCredentials> = {\n", profileType)
	fmt.Fprintf(&b, "  guard: %s,\n", quote(string(surfaceAuth.Guard)))
	fmt.Fprintf(&b, "  identifier: %s,\n", quote(surfaceAuth.Identifier))
	fmt.Fprintf(&b, "  login: (client, credentials) => client[%s]({ body: credentials }),\n", quote(loginOp.Name))
	fmt.Fprintf(&b, "  refresh: (client) => client[%s]({}),\n", quote(refreshOp.Name))
	fmt.Fprintf(&b, "  logout: (client) => client[%s]({}).then(() => {}),\n", quote(logoutOp.Name))
	fmt.Fprintf(&b, "  profile: (client) => client[%s]({}),\n", quote(profileOp.Name))
	fmt.Fprintf(&b, "};\n\n")
	fmt.Fprintf(&b, "/**\n * Creates an authenticated session and client for the %s portal.\n * Handles in-memory access tokens, cross-tab Web Locks, cookie refresh,\n * and 401 retry-and-renew transport interception.\n */\n", surface.Name)
	fmt.Fprintf(&b, "export function createPortalSession(\n  transport: HTTPTransport,\n  options?: PortalSessionOptions & { readonly clientOptions?: ClientOptions },\n): PortalSession<Client, %s, PasswordCredentials> {\n", profileType)
	fmt.Fprintf(&b, "  return runtimeCreatePortalSession(\n    authDescriptor,\n    (t) => createClient(t, options?.clientOptions),\n    transport,\n    options,\n  );\n}\n")

	return b.Bytes(), nil
}

// renderModules returns the directory's client files: shared runtime modules,
// the full entry and one entry per surface, each within the publication budget.
func renderModules(source *manifest.Manifest, prefix string, surfaces []Surface) (map[string][]byte, error) {
	document, err := source.Snapshot()
	if err != nil {
		return nil, err
	}
	names, err := newNaming(document)
	if err != nil {
		return nil, err
	}
	files := make(map[string][]byte, len(surfaces)+4)
	if files[moduleFile(prefix, "")], err = coreModule(); err != nil {
		return nil, err
	}
	if files[moduleFile(prefix, realtimeSource)], err = realtimeModule(prefix); err != nil {
		return nil, err
	}
	hasAuth := false
	for _, surface := range surfaces {
		if surface.Auth != nil {
			hasAuth = true
			break
		}
	}
	if hasAuth {
		if files[moduleFile(prefix, authSource)], err = authModule(prefix); err != nil {
			return nil, err
		}
	}
	// The full entry keeps its complete public API, including realtime types.
	if files[prefix+"_foundry.gen.ts"], err = entryModule(prefix, source, document, names, true); err != nil {
		return nil, err
	}
	for _, surface := range surfaces {
		projected, err := source.Project(surface.selection())
		if err != nil {
			return nil, err
		}
		view, err := projected.Snapshot()
		if err != nil {
			return nil, err
		}
		var authFragment []byte
		if surface.Auth != nil {
			authFragment, err = renderSurfaceAuth(prefix, surface, document, view, names)
			if err != nil {
				return nil, err
			}
		}
		if files[prefix+"_"+surface.Name+"_foundry.gen.ts"], err = entryModule(prefix, projected, view, names, view.Realtime != nil, authFragment); err != nil {
			return nil, err
		}
	}
	for _, data := range files {
		if len(data) > maxOutputBytes {
			return nil, safeerror.NewFault(fault.Invalid, "TypeScript output exceeds its publication byte budget")
		}
	}
	return files, nil
}
