package typescript

import (
	"bytes"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/generate"
)

// Runtime sources by module, in emission order. The core and realtime modules
// are shared by every entry in a directory; the entry sources depend on an
// entry's own operations and embedded manifest, so each entry inlines them.
var (
	coreSources  = []string{"wire", "formats", "validation_messages", "validation", "http"}
	entrySources = []string{"metadata", "descriptors", "forms"}
)

const realtimeSource = "realtime"

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

// entryModule renders one client entry from a full or projected manifest. It
// imports exactly the runtime names its code refers to and re-exports the
// public runtime API; the realtime API is re-exported when exportRealtime.
func entryModule(prefix string, source *manifest.Manifest, names naming, exportRealtime bool) ([]byte, error) {
	document, err := source.Snapshot()
	if err != nil {
		return nil, err
	}
	declared, err := runtimeNames()
	if err != nil {
		return nil, err
	}
	r := renderer{document: document, naming: names, uses: make(map[string]bool)}
	for _, name := range entrySources {
		data, err := runtimeSource(name)
		if err != nil {
			return nil, err
		}
		for line := range bytes.Lines(data) {
			if match := importLine.FindSubmatch(bytes.TrimRight(line, "\n")); match != nil {
				for imported := range strings.SplitSeq(string(match[2]), ",") {
					r.uses[strings.TrimSpace(imported)] = true
				}
			}
		}
		r.out.Write(withoutModuleLines(data))
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
			return nil, fault.New(fault.Internal, "generated client refers to an undeclared runtime name "+name)
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
	return out.Bytes(), nil
}

func index(set bool) int {
	if set {
		return 1
	}
	return 0
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
	files := make(map[string][]byte, len(surfaces)+3)
	if files[moduleFile(prefix, "")], err = coreModule(); err != nil {
		return nil, err
	}
	if files[moduleFile(prefix, realtimeSource)], err = realtimeModule(prefix); err != nil {
		return nil, err
	}
	// The full entry keeps its complete public API, including realtime types.
	if files[prefix+"_foundry.gen.ts"], err = entryModule(prefix, source, names, true); err != nil {
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
		if files[prefix+"_"+surface.Name+"_foundry.gen.ts"], err = entryModule(prefix, projected, names, view.Realtime != nil); err != nil {
			return nil, err
		}
	}
	for _, data := range files {
		if len(data) > maxOutputBytes {
			return nil, fault.New(fault.Invalid, "TypeScript output exceeds its publication byte budget")
		}
	}
	return files, nil
}
