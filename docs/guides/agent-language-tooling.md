# Go language tooling for agents

Milestone 24 adds JSON `timing.initialize_ns` and `timing.request_ns` to inspection
results. Startup/initialization and request/response time are separate; request
time includes gopls package loading caused by that request. Overall wall time also
includes source preparation and server shutdown. These timing additions passed
[milestone 24 acceptance](../production-acceptance.md); see
[native measurements](developer-resource-measurements.md).

Foundry's agent command speaks the Language Server Protocol (LSP) to an existing `gopls` executable. LSP is the editor protocol; gopls supplies Go completion, hover documentation, and definition locations. The command opens the actual consumer workspace, so its Go module graph and selected Foundry dependency determine the answers.

The client passes protocol fixture tests and real-gopls completion, hover, and definition acceptance against the independent consumer. The approved tool's version is pinned in [tools/gopls.version](../../tools/gopls.version), separately from framework runtime dependencies. Nothing installs gopls automatically.

Run `make gopls-install` with the native Go toolchain to install the pinned development tool into ignored `bin/`. The real acceptance gate is `FOUNDRY_TEST_GOPLS="$PWD/bin/gopls" make agent-smoke`. It checks completion, typed hover information, definition lookup and getter/mutator field documentation in the independent consumer. It also checks that source is unchanged. Ordinary tests skip that gate when no executable is selected; the smoke target fails if the selection is missing. A skipped test does not satisfy the real-language-server requirement.

## Inspect an existing consumer file

Run in the consumer workspace with a native gopls executable. With gopls available, an installed Foundry CLI accepts:

```sh
foundry agent hover --workspace /path/to/consumer --file models/user.go --line 12 --column 7
foundry agent definition --workspace /path/to/consumer --file models/user.go --line 12 --column 7
foundry agent complete --workspace /path/to/consumer --file handlers/users.go --line 24 --column 5 --insert 'fields.' --format text
```

These positions are illustrative; select a real position in your consumer file. In this framework checkout, `go run ./cmd/foundry agent ...` runs the same commands. `--gopls /absolute/path/to/gopls` selects an existing executable. The default executable is `gopls` on the host PATH.

`--line` and `--column` are one-based source coordinates. Columns count UTF-8 bytes, as Go source diagnostics do, and cannot split a Unicode character. The client converts positions to LSP's zero-based UTF-16 coordinates. `--insert` changes only the unsaved document buffer and requests information immediately after the inserted text. It never writes a probe into source or saves returned completion edits.

Use a context file inside the consumer workspace, including when symlinks are resolved. The buffer limit is 2 MiB, including inserted text. Multiline insertion is supported. Keep the buffer appropriate to a real Go expression or declaration; gopls decides which semantic results exist at that position.

## Results and process ownership

JSON is the default format. Its wrapper identifies the operation, workspace, source file, UTF-16 position, and server name/version, while `result` retains the actual LSP payload. Completion results can contain additional edits and documentation; they are returned as data. Text output prints completion labels/signatures with their documentation, hover text, or readable definition locations. In particular, [automatic field notices](model-accessors.md#notices-beside-the-actual-field) make existing getters and write mutators visible on ordinary model-field inspection. Markdown formatting in server documentation is retained.

Each invocation owns one server process, negotiates the protocol, opens and closes the unsaved document, then shuts the server down. `--timeout` defaults to 30 seconds. Cancellation terminates the owned process; shutdown has a separate bounded cleanup window. Server-requested workspace edits are refused. The command does not control another editor's gopls session.

Cleanup allows time for pending workspace analysis to stop. A caller deadline still interrupts that grace immediately. An unresponsive server is terminated, and errors identify shutdown-request, exit-notification or process-exit failures instead of presenting them as an API lookup failure. A successful LSP response does not hide unsuccessful cleanup.

An empty result is not an invented API answer. If generated code is stale, run generation first. If Go module resolution or the context is invalid, correct the consumer workspace and retry. Use the returned signature before calling an API, then use the consumer's compiler and tests as the final authority.

The client has protocol tests for framing limits, Unicode positions, unsaved buffers, server requests, errors, cancellation, and process cleanup. These fixture responses are explicitly distinct from real Go language-server verification. Protocol behavior follows the [LSP specification](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.17/specification/); the server is documented at [gopls](https://go.dev/gopls/).

## Verification batches and cache freshness

Milestone 23 adds batching to the acceptance harness. Each consumer editor
scenario starts one fresh gopls process and performs completion, hover and
lookup with separate owned overlays. Initialization and each operation have
independent deadlines. No server or unsaved state crosses scenarios. Every
existing assertion remains required; a completed response does not hide failed
cleanup. This harness change does not alter the single-operation CLI contract.

The independent consumer's named compiler-rejection tests retain one catalogue.
Selected cases run in bounded batches of twelve packages. Each case must have
its own failed build and expected diagnostic at its own invalid source file;
a dependency failure, runtime failure or another case's error cannot satisfy it.
Named `-run`/`-skip` selection remains supported.

`make test`, `make race` and `make fixture-check` compute one deterministic
`FOUNDRY_TEST_INPUTS` fingerprint per make invocation. It covers framework and
fixture sources, module metadata, generated manifests and selected Go/gopls/Node/
TypeScript tools. External-child acceptance tests, shared generator fixtures and
native doctor checks read it through `testkit.TrackExternalInputs`; the cache
acceptance test also reads the same key directly. Framework/tool changes therefore
invalidate cached consumer generation and compilation even when Go does not track
outside-module reads or a child process's compiled export data. Unchanged
inputs retain normal caching. Private `.env` files, caches and dependency trees
are excluded; explicitly selected tool packages are included. Source symlinks
are rejected instead of silently omitting their dependencies.

When directly running an external-child gate outside make, use `go test -count=1`:
the wrapper cannot fingerprint inputs for an arbitrary manual command. Existing
`agent-smoke` and `typescript-check` targets already do this. Finish a milestone's
source before the verification batch, then fix collected failures together.
Milestone 23's final native gate passed in 430.4s. The complete editor package
passed in 215.733s with 365 scenarios, compared with milestone 22's 582.512s
for 357 scenarios. The 915-case consumer compiler package passed in 9.366s,
compared with 130.414s for 903 cases. All actual assertions remain required.
These are observed acceptance timings under the default native compiler settings;
controlled resource measurements belong to milestone 24. Cache invalidation and
unchanged-input reuse were exercised with actual Go child processes.
