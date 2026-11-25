# Developer tooling and testing

Milestone 23 passed native verification, independent consumer review, relevant
races and bounded fuzzing. This guide explains the public APIs and their
executable consumer recipes.

<a id="agent-entry"></a>
## Agent entry

- [Author compiled guide examples](#recipe-author-examples)
- [Reference pinned guides](#recipe-reference-guides)
- [Read pinned guides offline](#recipe-read-pinned-guides)
- [Commands and bootstrap](#recipe-declare-command)
- [Individual artifact scaffolds](#recipe-scaffold-artifacts)
- [Doctor and declaration inspection](#recipe-inspect-declarations)
- [Test-owned application and transports](#recipe-test-transports)
- [Register model tables](model-declarations.md#recipe-register-tables)
- [Isolate service tests](isolated-http-tests.md#agent-entry)

<a id="recipe-author-examples"></a>
## Author compiled guide examples

Author executable recipes in the independent consumer fixtures, then include the
source in the guide that owns the explanation. Add a literal anchor immediately
before the recipe heading and a navigation-only Agent entry near the guide's top.
Agent entries contain only task labels and local guide links to explicit
`agent-entry` or `recipe-*` anchors. Put background links in the owning section
outside Agent entry. Entries contain no code or API summaries.

An anchor is a literal `<a id="anchor-id"></a>` on its own line, immediately
before its heading. IDs must be unique within the guide; heading text does not
create an explicit ID. An `agent-entry` or `recipe-*` anchor must bind to one ATX
heading (`#` through `######`); only blank lines may intervene. Displaced anchors,
consecutive anchors and setext headings at this boundary fail the check.
`make docs-check` validates local links with `agent-entry`
or `recipe-*` fragments through the shared `internal/docrefs` index. Other
fragments retain path-only checking; the checker does not emulate GitHub slugs.
Used Markdown reference-link destinations receive the same checks as inline links.

A source region starts with `// doc-example:begin <region>` and ends with
`// doc-example:end <region>`. Names use lowercase letters, digits and internal
hyphens and are unique within their file. Regions cannot nest or overlap. A boundary
must be an actual line comment in its source language. Marker-looking text inside
quoted or template literal text, Go raw strings, or block comments is rejected,
even when the enclosing file is a compiler input. A boundary
marker must stand alone. Put a blank line between a marker and a Go declaration
directive such as `//foundry:dto` so gofmt keeps their comment groups separate.

The guide names that region immediately before its fence:

````markdown
<a id="recipe-read-models"></a>
## Read models

<!-- compiled-source: tests/fixtures/consumer/example/query.go#read-models -->
```go
```
````

This syntax illustration names a hypothetical authoring path. Real directives
must name existing handwritten `.go` files under `tests/fixtures/consumer` or
`tests/fixtures/goway`, or registered TypeScript inputs in clientcontracts testdata.
Generated sources and symlink aliases are rejected. A directive without a fragment
means the entire source file, never a guessed excerpt.

The renderer changes only the next fence, with optional blank lines between the
directive and fence. It removes markers, trims a named region's leading and trailing
blank lines, then removes the longest byte-identical space/tab prefix shared by
nonblank lines. Interior blank lines remain and lose at most that prefix. Whole-file
inclusions retain their boundary blank lines. All other source lines and whitespace
remain intact. Render and read-only `--check` use these same rules.
A named region cannot begin or end with a bare `//` separator: move the marker
outside the comment group, or include the whole comment.
It requires the fence tag to match the source extension (`go` or `typescript`).
It rewrites only the bound fence body and preserves every other byte, including
the opening and closing delimiters. If an excerpt would close the fence, the
renderer asks the author to choose a longer delimiter.
It adds the newline needed to close a fence when a whole source file lacks one.
It never renames symbols, repairs calls or inserts ellipses.

A region may appear in several documents. Every Go, TypeScript or TSX fence in
a guide, an `AGENTS.md`, or `.claude/skills` Markdown requires the same binding.
The canonical fence tags are `go`, `typescript` and `tsx`. The framework source
reader accepts Go and TypeScript fixture files only; TSX has no framework host.
Every fenced block must declare exactly one supported tag: `go`, `typescript`,
`tsx`, `sh`, `toml`, `json`, `sql`, `text`, `ini`, `markdown`, `yaml` or `makefile`.
The closed list belongs to `internal/docsource`. Untagged fences, unknown tags,
attributes, indented code and HTML code blocks fail. Quotes and lists do not
exempt a block. The tag is the contract: code detection inside `text` and
`markdown` fences is best effort, including nonempty nested Go or TypeScript
fences. The other data and command tags remain a declared contract, without
content detection. Use `text` only for output and plain text; bind executable
examples to compiled source. There are
no per-document exceptions or directory-name exclusions inside teaching roots,
and those roots and their ancestors inside the repository cannot be symlinks.
Instruction, guide, skill, source and manifest inputs must be regular files.
The renderer opens files through a repository-confined filesystem root.

One Goldmark AST reader supplies block, container, link, anchor and inline syntax
to all documentation checks. A binding comment must be the immediately preceding
node in the same container. Comments containing apparent code remain comments, but
text or an element after a comment on the same line is checked like any other.
Every documentation command applies one size rule to every Markdown document it
reads: at most 512 KiB, except the dated historical records (`docs/evidence/` and
`CHANGELOG.md`, which the repository check reads for links), which may reach 1.5 MiB.
Before parsing, a document is refused with its line number when a line is longer than
16 KiB, opens with more than 16 quote or list markers, or ends with a lone carriage
return (CR-only line endings). Each source file is limited to 8 MiB and each
compiler-input manifest or stream to 16 MiB. Teaching-source loading (including rendered
bytes) and the anchor cache each allow at most 128 MiB per run, and a run reads at most
4096 documents or targets. Oversized inputs fail with the limit in the diagnostic.
Each document is parsed once per run and that parse serves its binding, retirement,
reference and link checks; retirement rules compile once, and each link target's
anchors are indexed once. In framework documents a code span holding only a bare name
that matches a retired member is reported as a match, not as that package's name; write
the qualified name you mean.

These binding, tag and block restrictions apply to the framework's documents.
An application's `foundry docs check` does not enforce them in this release.

`renderdocs --check` checks source equality. `checkdocs`, including its
`--go-inputs` mode, additionally reports every unbound block. Historical records in `docs/evidence`, blueprints and the changelog
are outside code-block coverage and cannot be guide or skill link targets.
Current guides and instructions teach `//foundry:model`; `checkdocs` rejects Go
examples that handwrite model tables, bindings or identity blocks.

A low-level generated-code or test reference can label one example with
`<!-- model-declaration-reference -->`, immediately before its `compiled-source`
marker and Go fence. Its source must be generated output, a `_test.go` file or
within the ORM owner tree. This allowance covers that block only; it does not
exempt a guide or change the application `models check` rule. API-name tables
and operational identity adapters do not declare models and need no marker.
Shell and JSON examples do not claim compiler proof.

Run `make generate` to render after declaration generation. For a guide-only edit,
`go run ./internal/cmd/renderdocs --guide model-queries.md` renders just that guide;
repeat `--guide` for another file. `make generate-check` compares without writing.
`make docs-check` checks equality, ownership, coverage, source paths, local links
and the explicit navigation anchors described above. Edit the fixture when a
rendered fence is wrong.

`make fixture-check` collects the actual `go list -json ./...` inputs from its
fixture modules using the same build environment as compilation. Ignored Go files,
unselected build variants and ignored testdata do not count. Test Go files count
because the gate compiles tests. Do not put a guide's only proof in a compiler-fail
fixture or an otherwise ignored package.

The main strict TypeScript fixture reads `clientcontracts/typescript-inputs.json`.
The table-bindings fixture reads its own `testdata/table-bindings/typescript-inputs.json`.
Those lists are actual compiler inputs, shared with documentation coverage, rather
than a separate snippet inventory. Add a new example to its matching list. Both
fixtures export current clients into temporary directories before strict compilation;
frozen clients remain compatibility evidence. Existing external-input fingerprints
include guides, fixture sources, input lists and selected native tools.

`make typescript-check` requires existing native tools in `FOUNDRY_TEST_NODE` and
`FOUNDRY_TEST_TYPESCRIPT`. `make verify` checks those selections and sets the existing required TypeScript mode
for its fixture run; it does not run the same client test a second time.

Rendered guides ship as readable Markdown. Source links name files in the matching
GitHub source tree; nested fixture modules need not exist in the installed module
cache. Reading a released recipe needs no compiler or renderer.

### Current API teaching

Guides teach only the current API. The retired-name ledger in
[`internal/retirement/denied.json`](../../internal/retirement/denied.json) records
retired names and optional replacement text in the same entries. The retired-names
ledger gives the replacement when one was documented; diagnostics print that text
or say that no replacement is recorded. Its replacement package names are checked
against current declarations. Historical labels
do not exempt a fence, table or paragraph from retirement checks.

<a id="recipe-reference-guides"></a>
## Reference pinned guides

Declare references on complete lines outside Markdown fences:

```markdown
Foundry guides: `http-endpoints`, `developer-tooling-and-testing#pinned-documentation-checks`
Foundry guides: `model-declarations`
```

Each line contains one or more backtick tokens, separated by commas. A token is
`guide-name` or `guide-name#anchor-id`. Names and IDs contain lowercase ASCII
letters, digits, and internal hyphens, with a letter or digit at each end.
Whitespace around tokens is allowed. Paths, extensions, URLs, empty fragments,
trailing commas, and trailing prose are invalid. Repeat the declaration for long
lists instead of wrapping a list onto an undeclared line. Backtick and tilde
fences can contain examples without declaring references.

`go tool foundry docs check` resolves each guide in the selected release and
validates every supplied fragment against its explicit IDs.
The framework's `make docs-check` uses the same parser and resolver for active
guides, instructions and `.claude/skills` Markdown, against its own guide tree.

<a id="recipe-read-pinned-guides"></a>
## Read pinned guides offline

Resolve the application's selected framework for offline reading:

```sh
GOWORK=off GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly \
	go list -m -f '{{.Dir}}' github.com/weiloon1234/Foundry-Go
```

Read `<resolved directory>/docs/guides/<guide-name>.md` and locate the literal ID
when the reference includes a fragment. Read prerequisites when the task needs
them. For GitHub, append `<guide-name>.md#<anchor-id>` to the application's pinned
`tree/<version>/docs/guides` base, using its commit for a pseudo-version. Do not
store module-cache paths in instructions or use sibling checkouts as authority.

<a id="pinned-documentation-checks"></a>
## Check documentation against the framework pin

From the application module, run:

```sh
GOWORK=off go tool foundry docs check --dir .
```

The command checks module-local `AGENTS.md` files and every Markdown file under
`.claude/skills`, including reference files and app-owned regions. It skips the
`.agents/skills` mirror, nested Go modules, dependency and build directories,
caches, test data, and records whose names contain a `YYYYMMDD` or `YYYY-MM-DD`
date. It does not need Git metadata. From a repository containing a nested app
module, use `GOWORK=off go -C system tool foundry docs check --dir .`.
`--dir` alone cannot select the module before `go tool` starts.
Application roots are resolved through symlinks before discovery and scanning.
Symlinks at `.claude`, `.claude/skills`, or within the active skills tree are
rejected; the `.agents/skills` mirror remains excluded.

The module and toolchain must already be cached. The checker disables downloads,
workspace selection, and module-file updates, and reports missing setup rather
than falling back to another release. It requires a verified match between the
running tool and selected framework module path and version, including versioned
replacements. Equal versions from different module paths fail. A global binary from a different
release, an unversioned build, or a local replacement fails. There is no version
bypass or mutation mode. The checker reads instructions and shipped guides; it
does not compile the application, render guide sources, run Node, or start
services. `go tool` may compile the checker into Go's normal build cache.

The selected release also owns retirement rules. The checker distinguishes
qualified owners and local constructor bindings, preserving valid scope
middleware, row locking, and exported session helpers. Application identifiers
such as codec names and React components can share a bare name with a retired
framework API. Bare names alone do not identify framework teaching in application
files. Qualified framework spellings and Go examples with framework owners inferred
from imports or known constructors still fail when they use retired APIs.
Application helpers such as `nullableJoinedText` are not framework retirement rules.
The framework's `make docs-check` retains strict checks for bare retired names.
Both checks exclude JSX elements and JavaScript, TypeScript, JSX, and TSX fences
from Go reference checks. Generic placeholders such as `Mutate<Field>` remain
documentation notation, not JSX elements. If a receiver's owner is unclear, the checker reports
ambiguous teaching. Qualify that owner or replace the example
with a guide reference. App-owned regions cannot exempt stale teaching by labeling
it an example. See the [compatibility policy](../compatibility.md#pinned-documentation)
for upgrades and rollback.

<a id="recipe-declare-command"></a>
## Commands and bootstrap

Declare an argument struct, bind it with `cli.Flags`, and pass the decoder to
`cli.Define`. `Command[A].Declare` accepts a constructor returning `cli.Handler[A]`;
the compiler retains the same argument type through decoding and execution.
`cli.New` freezes explicit declarations and rejects duplicate names. `Parse`
validates arguments and prints help before application startup. The selected
handler is constructed only when its invocation runs.

<!-- compiled-source: tests/fixtures/consumer/tooling/commands.go#typed-command -->
```go
type Greeting struct {
	Name  string
	Count int
}
type Greeter struct{ Prefix string }

var GreeterKey = foundation.NewKey[*Greeter]("tooling.greeter")
var CommandsKey = foundation.NewKey[*cli.Registry]("tooling.commands")

var Greet = cli.Define("greet", "Write a typed greeting", cli.Flags(func(flags *flag.FlagSet, args *Greeting) {
	flags.StringVar(&args.Name, "name", "", "recipient name")
	flags.IntVar(&args.Count, "count", 1, "number of greetings")
}, func(args Greeting) error {
	if args.Name == "" || args.Count < 1 || args.Count > 10 {
		return cli.Usage("--name and a count from 1 to 10 are required")
	}
	return nil
}))

func GreetingDeclaration() (cli.Declaration, error) {
	return Greet.Declare(func(resolver foundation.Resolver) (cli.Handler[Greeting], error) {
		service, err := foundation.Resolve(resolver, GreeterKey)
		if err != nil {
			return nil, err
		}
		return func(ctx context.Context, args Greeting, streams cli.Streams) error {
			for range args.Count {
				if err := ctx.Err(); err != nil {
					return err
				}
				if _, err := fmt.Fprintf(streams.Out, "%s %s\n", service.Prefix, args.Name); err != nil {
					return err
				}
			}
			return nil
		}, nil
	})
}
```

<!-- compiled-source: tests/fixtures/consumer/tooling/commands.go#parse-before-bootstrap -->
```go
func Run(ctx context.Context, args []string, streams cli.Streams) error {
	greeting, err := GreetingDeclaration()
	if err != nil {
		return err
	}
	var registry *cli.Registry
	var builder *foundation.Builder
	inspect, err := inspection.Command(InspectName, func(ctx context.Context) (inspection.Report, error) {
		return inspection.Collect(ctx, inspection.Sources{Builder: builder, Commands: registry})
	})
	if err != nil {
		return err
	}
	registry, err = cli.New(greeting, inspect)
	if err != nil {
		return err
	}
	invocation, err := registry.Parse(args, streams.Out)
	if err != nil {
		return err
	}
	service := foundation.Module{Name: "greeter", OnRegister: func(r *foundation.Registrar) error {
		return foundation.Provide(r, GreeterKey, &Greeter{Prefix: "Hello"})
	}}
	builder = foundry.New().Register(service, cli.Module("cli", CommandsKey, registry, invocation, streams, service.Name))
	if invocation.Name() == InspectName {
		return invocation.Run(ctx, nil, streams)
	}
	app, err := builder.Build(ctx)
	if err != nil {
		return err
	}
	return app.Run(ctx, foundation.CLI)
}
```

The [command consumer](../../tests/fixtures/consumer/tooling/commands.go) is the
complete recipe: typed flags, validation, injected services, explicit streams,
shared `cli.Module` bootstrap and a metadata command. Domain commands use the
normal CLI kernel and provider shutdown. An invocation runs once, including on
failure; parse another invocation for an intentional retry. Cancellation before
execution does not acquire that execution claim. `cli.Flags` rejects positional
arguments; `cli.FlagsWithArgs(configure, positional, validate)` binds the values
remaining after flags (and after `--`) into the same argument struct, and
`cli.ExactArgs(n, assign)` enforces a count. Positional errors are usage errors.
A custom decoder can still implement nested subcommands without a second registry.

Destructive commands can call `cli.Confirm(ctx, streams, prompt, force)`, which
accepts only `y`/`yes` from an interactive input and refuses a non-terminal file
input (pipes, `/dev/null`) with `cli.ErrNotConfirmed` unless `force` is set;
`cli.ConfirmInProduction` asks only when its `production` argument is true.
`cli.WriteTable(output, header, rows)` writes aligned columns and replaces control
characters inside cells, so data cannot forge rows or columns.

Commands are rejected while application maintenance is paused. Declare an
operational command with `Command.AllowDuringMaintenance()`, or run one invocation
with the leading `--during-maintenance` flag (`app --during-maintenance migrate`);
draining still rejects both. `application.MaintenanceCommands()` returns the `down`
and `up` declarations (see [production operations](production-operations.md)), and
`application.AboutCommand(name, settings)` prints the framework version, Go
toolchain, platform, namespace, timezone, enabled kernels and configured service
drivers (names and driver kinds only; hosts, users and credentials are omitted) as a
table or `--json`, without building the application.

`cli.Report` writes an error and returns a status for the application's main:
0 for success/clean help, 1 for execution/reporting failure, 2 for invalid usage,
124 for deadline expiry and 130 for cancellation. The framework library never
calls `os.Exit`. The development binary owns signal cancellation and its exit.
Status inspection bounds cyclic or excessively large/deep error graphs and
reports them as execution failures. Custom error methods must return and make
shallow `Is`/`As` comparisons; panic and Goexit stay inside owned isolation.
Handlers and constructors must honor context cancellation; the framework contains
panic/Goexit, but cannot forcibly stop arbitrary application goroutines.

## Development server supervision loop

`devserver.Run(ctx, opts, streams)` runs the application development loop. It watches
Go sources and declared application inputs, settles change bursts, rebuilds the target
binary with `-ldflags=-w`, and restarts the server child process. It also adds
`-gcflags=all=-dwarf=false` unless the inherited `GOFLAGS` contains `-gcflags` or
`--gcflags`, either as a separate field or followed by `=...`.
If compilation fails, the previous server keeps running uninterrupted.

Configure `devserver.Options`. Supply `validatePortals` as the application callback
that checks portal folder layout before a rebuild:

<!-- compiled-source: tests/fixtures/consumer/tooling/guide_devserver.go#developer-tooling-and-testing-1 -->
```go
func runDevelopmentServer(ctx context.Context, projectRoot, localEnvPath string, streams cli.Streams, validatePortals func() error) error {
	opts := devserver.Options{
		Root:    projectRoot,
		Target:  "./cmd/artisan",
		Args:    []string{"serve"},
		Env:     []string{"WEB__ENABLED=true", "WEB__DEV_PROXY=true"},
		Watched: []string{"config/*.toml", "web/portals/*/portal.json", localEnvPath},
		Helpers: []devserver.Helper{
			{
				Name:       "admin",
				Executable: "npm",
				Args:       []string{"--prefix", "web", "run", "dev", "--", "admin", "--port", "5173", "--strictPort"},
				Banner:     "admin: http://127.0.0.1:8080/admin (Vite on 5173)",
			},
		},
		OnChange: validatePortals,
	}
	if err := devserver.Run(ctx, opts, streams); err != nil {
		return err
	}
	return nil
}
```

Standard module conventions (root `*.go`, `cmd/`, `internal/`, `go.mod`, `go.sum`) are
watched automatically while ignoring test sources, hidden directories, `node_modules`,
`testdata` and `vendor`. Extra paths in `Watched` support directories, explicit files and
glob patterns relative to `Root`. Every child process runs in an isolated process group;
terminal disconnect (`SIGHUP`) and context cancellation terminate all child groups with
`SIGTERM`, a 5-second drain window, and `SIGKILL` process reaping. `OnChange` runs inside
isolated callback containment (`internal/callback`) so application panics or `runtime.Goexit`
do not crash the supervisor unhandled.

<a id="recipe-scaffold-artifacts"></a>
## Individual artifact scaffolds

If the application console cannot compile because generated types are missing,
run `GOWORK=off go tool foundry generate --recursive --dir .` directly.
Generated files and their ownership manifests remain generator-owned.


For public signatures, run `GOWORK=off go doc <package>` or
`GOWORK=off go doc <package>.<Symbol>` in the application module. For semantic
lookup in a consumer file, use [agent inspection](agent-language-tooling.md#recipe-inspect-source).

Prefer the [project-pinned CLI](team-adoption.md) through `go tool foundry`, which
shares the application's framework requirement. An installed `foundry` must match
that dependency. In this checkout the same command is `go run ./cmd/foundry`.
Select an existing Go package directory inside the consumer module. For a new
package, first add a `doc.go` containing its package declaration, as shown in
[team adoption](team-adoption.md). An empty or missing directory is not a Go package.

Then create individual declarations:

```sh
# Add a model struct with the directive from the model-declaration guide.
GOWORK=off go tool foundry make dto SaveWidgetRequest --http body --dir ./transport
GOWORK=off go tool foundry make dto WidgetResource --role response --http response --dir ./transport
GOWORK=off go tool foundry make job DeliverWidget --id widget.deliver --queue deliveries --dir ./jobs
GOWORK=off go tool foundry make command InspectWidgets --id widgets.inspect --dir ./commands
GOWORK=off go tool foundry generate --recursive --dir .
GOWORK=off go tool foundry generate --recursive --check --dir .
```

The other kinds follow the same pattern:

| Command | Creates |
| --- | --- |
| `make endpoint CreateNote --id notes.create --method POST --path /notes` | `CreateNoteRequest`/`CreateNoteResponse` DTOs (no request DTO for `GET`), their generated codecs, then the typed route, `CreateNoteEndpoint()` and a `HandleCreateNote` stub. Paths are static; add a `//foundry:path` struct for parameters afterwards. |
| `make enum NoteState --cases draft,published` | A `//foundry:enum` string type with one constant per case. |
| `make event NotePublished --id notes.published` | A payload struct and its `events.Topic`. |
| `make listener IndexNote --id notes.index --event NotePublished` | An `events.Listener` for an existing payload type and its handler stub. |
| `make policy UpdateNote --id notes.update --subject Member --resource Note` | An `auth.Policy` that denies until implemented. |
| `make middleware Audit --id notes.audit` | A pass-through `http.Middleware`. |
| `make rule Slug --id notes.slug` | A `validation.Custom` string rule. |
| `make notification NoteShared --id notes.shared` | A `NoteSharedPayload` DTO, its codec and the `notifications.Definition`. |
| `make migration CreateNotes --id ... --origin app --version v1.0.0 --create notes` | A migration starting from a reviewed `CREATE TABLE` with a UUID key and timestamps. |
| `make model Article --table articles --translated Title,Summary --attachment Logo --attachments Galleries --metadata SEO --disk public` | A model struct with the [model directive](model-scaffolding.md); run `foundry generate` after scaffolding. It includes a model with [extension slot](model-extension-slots.md) fields: `translations.Text`, `attachments.One`/`Many` and `metadata.Value` of a new empty `ArticleSEO` DTO. Attachment slots add an `ArticleDisk` declaration of the named storage disk and handwritten `ArticleExtensions` handles whose policies accept nothing until you list their media types, so assembly refuses them until then. `--disk` is required exactly when attachment slots are requested. |

Endpoint and notification scaffolds check every target first, then create each
DTO, run generation for the package and create the component file; each step
publishes through the same guarded path, so a later failure leaves earlier,
valid files in place. Test files and factories remain handwritten: they are not
package declarations the scaffold type checker can verify. Model scaffolds write
one directive struct, extension policies and a per-model registration helper.
Like DTO scaffolds, they print the next generation command. Preflight checks the
proposed source together with its generated table, identity and metadata codecs
in memory. Endpoint and notification scaffolds run generation using the tool's
framework build; other scaffolds leave generation to the caller.

The generic DTO scaffold accepts `--http body`, `--http response` or `--http both`.
Omit it to retain the plain `//foundry:dto` output. `--role response` independently
omits request validation; together with `--http response` it writes
`//foundry:dto role=response http=response`. `--role row` writes
`//foundry:dto role=row` for a datatable row; generation then emits `XColumns()`
bindings instead of validation fields, and `--role row` rejects `--http body` and
`--http both`. Application command aliases can forward
these flags to `foundry make dto`. Internal tooling uses `ScaffoldOptions.HTTP`
and `ScaffoldOptions.Role`. Unknown values, framework-module destinations and
helper-name collisions are rejected before the scaffold writes a file. Helpers
are checked in memory; run generation after adding the DTO fields to publish them.

These commands create one consumer-owned artifact, never an application project.
Existing migration/seeder scaffolds use the same path checks and publisher.
Keep model field documentation in the handwritten struct; generate its handles.
Models require an explicit table. DTO identity comes from its Go type. Jobs and
commands require semantic IDs. Add fields/behavior to the handwritten artifact,
then run generation for model tables and DTO codecs. Model field or tag edits require regeneration. Job and command placeholders
return an explicit error until implemented and registered. Scaffolds never
silently overwrite existing files; recovery preserves create-only authority.

Fresh-package generation, installed dependency compatibility and collisions are
covered by the [scaffold tests](../../internal/generate/scaffold_developer_test.go)
and the [component scaffold tests](../../internal/generate/scaffold_components_test.go).

<a id="recipe-inspect-declarations"></a>
## Doctor and declaration inspection

Inspect declared Redis key prefixes after composition, before Build or service
startup. Serialize the returned inventory rather than reconstructing prefixes.
`application.RedisKeysCommand` adds table and JSON output to the same CLI registry.

<!-- compiled-source: tests/fixtures/consumer/bootstrap/redis_inventory_test.go#redis-inventory -->
```go
inventory, err := builder.RedisKeys()
if err != nil {
	t.Fatal(err)
}
if len(inventory) != 1 || len(inventory[0].Keys) != 2 {
	t.Fatal(inventory)
}
```

```sh
GOWORK=off go tool foundry doctor --dir . --go go --gopls gopls --python python3 --require-gopls
```

Doctor reads the selected Go/module metadata and optionally checks gopls.
The optional `deploy-tool` check runs the pinned deploy tool's `help` with the
Python selected by `--python`. Doctor compares actual module Go requirements,
bounds each process and its output, disables downloads, and reports actionable
required-check failures. Its
`tool-version` check compares the framework release the running `foundry` was
built from with the version the module selects; a known mismatch fails, while
development builds and local `replace` directories are reported as unverifiable.
`foundry generate` rejects the same mismatch before analysis. It does not
compile consumer packages, start services, load application configuration or
install tools. Omit `--require-gopls` when editor tooling is optional.

`inspection.Collect` accepts existing route, job, schedule, CLI and contract
registries, a builder and configuration reports. It reuses their metadata and
returns owned snapshots. Builder inspection invokes repeatable registration
callbacks; it does not construct services or boot providers. Registration must
therefore remain declarative. Configuration output contains names, sources and
secret markers, never setting values.

`inspection.Command` accepts a pure collector so it can include the final CLI
registry itself. The consumer runs this invocation before `Build`/`Run`, keeping
metadata inspection independent of service startup. Select a typed section
(`routes`, `jobs`, `schedules`, `plugins`, `commands`, `configuration`, `contracts`, `extensions`
or `all`) and compact JSON or readable indented text. The development binary
cannot import an arbitrary application's bootstrap: register this command in the
application that owns those declarations.

<a id="diagnostic-rules"></a>
## Read a check finding

Every compiler-independent check prints each finding as one line of one shape:

```text
path:line:col: [rule] subject: problem; fix. Read docs/guides/<guide>.md#<anchor> (skill: name)
```

`:col` is omitted when a check has no column, and `:line` when it has no line; a
zero is never printed. `subject:` is omitted when the problem sentence begins with
the public name. `Read` names the guide page and explicit anchor that teach the fix,
as a path inside the pinned framework (see [Read pinned guides offline](#recipe-read-pinned-guides)).
`(skill: name)` is added by the commands that run in an application when
`.claude/skills/*/SKILL.md` declares that `guide#anchor` in a `Foundry guides:`
line; it names every such skill, in name order. A rule no skill declares prints the
guide only. The command reads the skills once per run, and only when it has a finding.

The rule IDs are stable output. `foundry routes check`, `foundry models check`,
`foundry lint --no-raw`, `foundry generate --check`, `foundry generate`,
`foundry docs check` and the framework's `make docs-check` print these rules, and a
rule's guide and anchor are validated by `make docs-check`:

| Rule | Check | Teaching |
| --- | --- | --- |
| `route-nesting` | `routes check` | `flat-routes#staged-routes` |
| `route-helper` | `routes check` | `flat-routes#generated-helpers` |
| `route-helper-missing` | `routes check` | `flat-routes#generated-helpers` |
| `model-table` | `models check` | `model-declarations#recipe-declare-table` |
| `raw-sql` | `lint --no-raw` | `database-runtime#no-raw-policy` |
| `generated-stale` | `generate --check` | `model-declarations#recipe-stale-declarations` |
| `generate-declaration` | `generate` | `model-declarations#diagnose-declarations` |
| `guides-declaration` | `docs check` | `developer-tooling-and-testing#recipe-reference-guides` |
| `anchor-invalid` | `docs check` | `developer-tooling-and-testing#recipe-author-examples` |
| `anchor-duplicate` | `docs check` | `developer-tooling-and-testing#recipe-author-examples` |
| `anchor-missing` | `docs check` | `developer-tooling-and-testing#recipe-reference-guides` |
| `guide-name` | `docs check` | `developer-tooling-and-testing#recipe-reference-guides` |
| `guide-missing` | `docs check` | `developer-tooling-and-testing#recipe-reference-guides` |
| `retired-command` | `docs check` | `developer-tooling-and-testing#pinned-documentation-checks` |
| `retired-api` | `docs check` | `developer-tooling-and-testing#pinned-documentation-checks` |
| `ambiguous-api` | `docs check` | `developer-tooling-and-testing#pinned-documentation-checks` |
| `doc-limit` | `docs check` | `developer-tooling-and-testing#pinned-documentation-checks` |
| `doc-file` | `docs check` | `developer-tooling-and-testing#pinned-documentation-checks` |
| `doc-comment` | `docs check` | `developer-tooling-and-testing#recipe-author-examples` |
| `doc-rendered` | `checkdocs` | `developer-tooling-and-testing#recipe-author-examples` |
| `doc-marker` | `checkdocs` | `developer-tooling-and-testing#recipe-author-examples` |
| `doc-region` | `checkdocs` | `developer-tooling-and-testing#recipe-author-examples` |
| `doc-binding` | `checkdocs` | `developer-tooling-and-testing#recipe-author-examples` |
| `doc-section-anchor` | `checkdocs` | `developer-tooling-and-testing#recipe-author-examples` |
| `doc-agent-entry` | `checkdocs` | `developer-tooling-and-testing#recipe-author-examples` |
| `doc-inputs` | `checkdocs` | `developer-tooling-and-testing#recipe-author-examples` |
| `doc-link` | `checkdocs` | `developer-tooling-and-testing#recipe-author-examples` |
| `doc-milestone` | `checkdocs` | `developer-tooling-and-testing#recipe-author-examples` |
| `doc-model-example` | `checkdocs` | `model-declarations#recipe-declare-table` |
| `fixture-go-version` | `checkdocs` | `developer-tooling-and-testing#documentation-gate` |
| `blueprint-numbered` | `checkdocs` | `developer-tooling-and-testing#documentation-gate` |
| `rule-catalogue` | `checkdocs` | `developer-tooling-and-testing#diagnostic-rules` |

<a id="documentation-gate"></a>
The `fixture-go-version` and `blueprint-numbered` rules belong to the framework's own
`make docs-check`: a fixture module's `go` directive must equal the root `go.mod`, and
`blueprint/` files are numbered `NN-name.md` without gaps or repeats.

<a id="recipe-test-transports"></a>
## Test-owned application and transports

`testkit.Start` uses the actual builder and lifecycle and registers cleanup before
startup. Partial boot failure and later `t.Fatal` still release registered
resources. Register application cleanup before clients so clients close first.
A [failing-child-test acceptance](../../testkit/cleanup_test.go) checks this boundary.

`testkit/http.New` serves the supplied production handler on an owned local
server. Its embedded production `httpclient.Client` uses bounded requests and no
retries or redirect following. Each request is limited to `DefaultTimeout` (five
seconds); pass `WithTimeout` to `testkit/http.New`/`testkit/http.Connect` for slow handlers, and
`WithHeaders` for headers on every request. On a derived client each named
header replaces the inherited value rather than adding a second one. `client.With(t, options...)` derives
an independent client for the same server. `JSON`, `DecodeJSON` and
`AssertStatus` keep generated request/response contracts explicit. Requests
traverse the handler's real middleware and auth.
See the [HTTP recipe](../../tests/fixtures/consumer/tooling/tooling_test.go) and
[test-owned kernels](isolated-http-tests.md#recipe-run-kernels).

Response assertions report failures through the test without printing secrets:

| Helper | Checks |
| --- | --- |
| `AssertJSONPath(t, response, "/data/items/0", want)` | The value at an RFC 6901 pointer equals `want` encoded by Go's JSON encoder, ignoring key order. |
| `AssertValidationErrors(t, response, "email", "/items/0/name")` | A 422 `validation_failed` envelope with an issue at each wire path. |
| `AssertHeader`, `AssertCookie` | A header value; exactly one `Set-Cookie` for a name, returned for attribute checks. |
| `AssertRedirect`, `AssertNoContent` | A 3xx with an exact `Location`; a 204 with an empty body. |

`ActingAsToken(t, client, tokens, proof, options)` issues a real access token
through the application's configured `token.Tokens` binding and returns a client
sending it as a bearer credential, replacing any inherited `Authorization`.
`ActingAsSession` issues a real session and sends it in the declared credential
cookie, replacing an inherited cookie of that name and keeping other cookies in
one `Cookie` header. Re-authenticating a client therefore never sends two
credentials, which the framework rejects as ambiguous. Build `proof` with `auth.NewProof`
for the actor and assurance under test. Every request still verifies the
credential, provider eligibility, scopes, policies and, for cookies, the
application's origin/CSRF policy; the helpers grant nothing themselves.

`testkit/websocket.Connect` owns a real connection and handshake. Send protocol
requests, receive bounded frames, assert their type and decode event payloads
with `DecodePayload` and the generated DTO contract. The
[realtime consumer](../../tests/fixtures/consumer/realtime/orders_test.go) exercises
this through a real kernel, native middleware, broadcast and replay.

## Typed factories and explicit fakes

The [factory recipe](../../tests/fixtures/consumer/tooling/database.go) returns
`Factory[models.WriteRecord, []orm.Assignment[models.WriteRecord]]`.
The same pattern is compiled for users:

<!-- compiled-source: tests/fixtures/goway/guideadapters/examples_test.go#developer-tooling-and-testing-users-factory -->
```go
func usersFactory() (*factory.Factory[models.User, []orm.Assignment[models.User]], error) {
	return factory.Define(models.Users.Table, func(_ context.Context, n factory.Sequence) ([]orm.Assignment[models.User], error) {
		return []orm.Assignment[models.User]{
			orm.Set(models.Users.Email, fmt.Sprintf("reader-%d@example.test", n)),
			orm.Set(models.Users.Age, 18),
			orm.Set(models.Users.Status, models.StatusActive),
			orm.Set(models.Users.Level, models.LevelBasic),
		}, nil
	})
}
```

`Draft` builds without SQL;
`WithStates` appends ordered typed transformations. Derived states share a
monotonically increasing sequence; separate factories own independent sequences.
Failed builds consume their number. Shared callbacks must support concurrency.

`Create` and `CreateMany` use normal ORM mutation preparation and model
lifecycle hooks. The bounded batch uses per-model `InsertEach`, with atomic
rollback and real after-commit timing. It never switches to a hook-free bulk
insert. The [PostgreSQL factory consumer](../../tests/fixtures/consumer/softqueries/factory_postgres_test.go)
checks defaults, UUIDs, mutators, timestamps, observer order and rollback.

Composition stays typed:

- `DraftWith(ctx, overrides...)` and `CreateWith(ctx, writer, overrides...)`
  apply per-call `State` overrides after the factory's own states without
  retaining them, using `factory.State[[]orm.Assignment[M]]`. Replace an existing assignment
  to override a field; duplicate assignments are rejected.
- `AfterCreating(hooks...)` derives a factory whose hooks run in order after each
  created model, inside the transaction that inserted it (a savepoint within
  the caller's transaction); a hook can create related records or return a
  reloaded model. A failing or panicking hook rolls back its model and, for
  `CreateMany`, the whole batch, even with a pool writer. Hooks are bounded by
  `MaxHooks` and contained like other callbacks.
- `factory.CreateFor(ctx, writer, child, parent, assign)` creates a parent through
  its factory and one child that belongs to it; `factory.CreateHas(ctx, writer,
  owner, child, count, assign)` creates an owner and a bounded batch of children.
  `assign` adds an assignment carrying the parent's key to the child's inputs.

`factory.NewFaker(seed)` is a small deterministic generator of fictional values
(`Name`, `Email` at the reserved `example.test` domain and unique per faker,
`Word`, `Words`, `Sentence`, `IntBetween`, `Float`, `Bool`, `TimeBetween`, and
`factory.Pick`). The same seed reproduces a failing test's values; it uses only
the standard library.

`testkit/dbassert` asserts persisted state through the typed ORM queries
applications already use: `AssertExists`, `AssertMissing`, `AssertCount` and
`AssertSoftDeleted` take typed callbacks and the test's executor or transaction.
For example, pass `func(ctx context.Context, ex database.Executor) (bool, error) { return orm.Exists(ctx, ex, q) }`
to `AssertExists`. `AssertMissing` and `AssertCount` take a count callback using
`orm.Count`; `AssertSoftDeleted` takes the ordinary count and an existence callback
using `q.OnlyTrashed()`. Model scopes,
including soft-delete visibility, apply as in application code; the helpers only
read and never reset or seed data.

The [local capability recipe](../../tests/fixtures/consumer/tooling/helpers_test.go)
uses explicit helpers with production contracts:

| Helper | Behavior |
| --- | --- |
| `testkit.NewClock` | Independent controllable application time; context deadlines still use real time. |
| `testkit/telemetry.Flush` | Captures and persists pending telemetry synchronously without starting a kernel loop. Uses normal receipts, limits and incident evaluation; see [persisted telemetry testing](persisted-telemetry.md#testing). |
| `testkit/storage.Local` | Real local disk in a test-owned temporary directory, closed by cleanup. |
| `testkit/email.New` | Bounded memory driver used by the normal mailer; `AssertSent`, `AssertNotSent` and `AssertSentCount` take typed message predicates; assertions omit private message content. |
| `testkit/jobs.New` | Independent memory backend and namespace, real declarations, typed capture/dispatch and deduplication. Workers run only when explicitly started. `AssertPushed`, `AssertNotPushed` and `AssertPushedCount` decode retained payloads of a typed definition and apply a typed predicate without printing payloads. |
| `testkit/events.New[E]` | Bounded immutable listener snapshots, normal bus dispatch and caller-owned transaction/after-commit semantics. |
| `testkit/events.NewFake` | Intercepts a production bus so listeners do not run; `AssertDispatched`, `AssertNotDispatched` and `AssertDispatchedCount` take typed topics and payload predicates. |
| `testkit/notifications.New[D]` | Recording `notifications.Custom` transport with a configurable outcome (`Respond`), so tests exercise the production manager and claim protocol; `AssertDelivered`, `AssertNotDelivered` and `AssertDeliveredCount` take typed output predicates. |
| `testkit/auth.Scope` / `Require` | Existing real verifier/guard/policy behavior; helpers do not bypass eligibility or MFA requirements. |

Use `testkit.Namespace` for an independent cache/coordination namespace. Existing
PostgreSQL helpers create isolated identities/records; they never authorize
resetting shared data. No helper replaces global state or resets a database.

## Query plans and acceptance workflow

[Typed PostgreSQL query plans](query-plans.md) reuse compiled SQL and bindings;
ordinary inspection and explicit execution analysis have separate methods.

Finish a milestone's implementation, tests, examples and docs before compilation
or testing. Then run the full milestone batch, collect failures, fix them as a
batch and repeat affected checks. Accept only after final-source `make verify`
and required integration/race/editor checks pass. The
[language tooling guide](agent-language-tooling.md#verification-batches-and-cache-freshness)
explains scenario batching and external-input cache freshness. Skipped external
checks and unexecuted tests are not acceptance evidence.

## Continuous verification and security

The repository workflow (`.github/workflows/verify.yml`) selects gates from changed
paths. Changes under `tools/deploy/`, `tests/fixtures/deploy-acceptance/`, or
`.github/` enable deployment jobs. Any change outside `tools/deploy/` enables the
backend race tier and security scan. Schedules, manual runs, tags, release branches,
and unavailable change history enable every gate. Pull requests skip the container
and VM acceptance jobs.

The macOS `verify` job always runs the full `make verify` with the `verify` setup
profile. Go and deployment unit checks depend on each other's source and fixtures,
so path selection never skips either side's unit checks.
For Go changes, two `backends` shards run `make test-postgres` with
`TEST_POSTGRES_SHARD=1/2` and `2/2`; only shard 1 also runs `make test-redis`.
The Makefile's `RACE_HEAVY_PACKAGES` lists heavy import paths, heaviest first.
The PostgreSQL runner assigns them round-robin using their positions in that list
across all three modules. Each module ignores entries it does not contain, runs
its assigned heavy packages first, then deals its remaining sorted packages
round-robin starting at shard 1. A test rejects heavy entries absent from all
three modules. Local runs default to `1/1` and retain the full sorted execution
order. The Makefile's `RACE_EXEMPT_PACKAGES` list removes
packages from `make race` and race-enabled PostgreSQL checks, while `make test`
retains their ordinary coverage. The exemptions are `internal/retirement`,
`internal/noraw`, and `cmd/foundry`. A parser guard rejects missing exempt packages,
`go` statements, calls to methods named `Go`, and errgroup imports. It checks every
non-test Go file regardless of build tags.

The `security` job runs `make security-check` and uploads its evidence for Go
changes. Verification and backend jobs start isolated runner-local PostgreSQL 18
and Redis. Security needs no services.
The shared setup action (`.github/actions/setup-foundry/action.yml`) installs the
root `go.mod` toolchain and each job's required tools. Required backend, real gopls
and TypeScript checks remain enabled. Each profile and backend shard restores its own newest Go
build and module cache from `main` without waiting for the other gates. A
successful `main` run then saves a fresh copy keyed by its commit
(`.github/actions/save-go-cache`), keeping only the build cache entries that run
used, so the cache follows the current source instead of growing. Pull requests
and tags only restore, so they cannot evict `main`'s caches from the repository's
10 GB cache budget. After the v0.2
generic-method reduction and ORM test repartitioning, every cold `go vet ./...`
compile stays under 2 GB peak RSS (largest: `database/orm` at 1.82 GB and
`database/orm/rows` at 1.54 GB), so the 7 GB macOS runners use default parallel
compilation without `GOFLAGS=-p=1`; see the [CI budget](developer-resource-measurements.md#developer-and-ci-budgets).
Both `Makefile` and CI configure canonical `GOFLAGS` containing `-trimpath -gcflags=all=-dwarf=false`, omitting compiler DWARF from development and test archives.
Make preserves caller-provided `-gcflags`; use `GOFLAGS=-gcflags=all=-dwarf=true make test` to restore DWARF.
For Delve, run `GOFLAGS=-gcflags=all=-dwarf=true dlv debug ./cmd/artisan` separately from devserver so Delve supplies its debug flags without devserver's `-ldflags=-w`.
Without `-trimpath`, Go includes absolute checkout directories in build action
IDs, causing each checkout path or worktree to build separate cache copies and
duplicate `GOCACHE`. With `-trimpath`, recorded source paths are module-relative,
enabling different checkouts, worktrees, and CI runners to share build cache
entries safely.
Dependency update configuration (`.github/dependabot.yml`)
keeps proposals reviewable; it does not automatically merge or publish them.

The root `go` directive remains the consumer minimum. Its `toolchain` directive
selects the patched development SDK; Makefile exports that selection to nested
fixture and release-tool modules without duplicating the SDK pin. For direct
commands in nested modules, export `GOTOOLCHAIN` from the root first:

```sh
export GOTOOLCHAIN="$(awk '$1 == "toolchain" { print $2 }' go.mod)"
```

`make security-check` is independent of cold package measurements. Select existing
`FOUNDRY_TEST_GOVULNCHECK`, `FOUNDRY_TEST_GOPLS`, `FOUNDRY_TEST_NODE` and
`FOUNDRY_TEST_NPM` executables; the command installs nothing and reuses warm caches.
It scans the framework, release tool, all independent fixtures, the selected gopls
binary and the TypeScript lockfile. `SECURITY_OUTPUT` selects its evidence directory
(default `.cache/security-check`). Each report retains scanner/database metadata,
actual findings and failure dispositions. Known affected functions, npm findings,
missing/malformed results and tool failures fail the gate even when JSON-mode
scanner exit status is zero. Module/package-only findings are preserved for review.
See the [security policy](../../SECURITY.md) for private disclosure.

## Optional frontend adapter acceptance

`tools/typescript/package.json` declares development-only React, React DOM, Vue,
DOM test support and declaration packages alongside the pinned compiler. Install
these with the repository's normal dependency-approval policy before requiring
`make typescript-check`. The core generated SDK has no runtime npm dependencies.
The client gate compiles/runs ordinary TS/JS HTTP/WebSocket contracts and the
optional real React/Vue adapter checks. Missing peers fail required-mode checks.
External-input fingerprints include the selected compiler's dedicated
`node_modules` tree, including transitive adapter implementations and types.


The ORM examples are checked against the [compiled guide examples](../../tests/fixtures/goway/guideadapters) and the linked consumer sources.
