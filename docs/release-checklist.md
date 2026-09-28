# Release checks

Milestone 24 completed the [recorded local candidate acceptance and framework
audit](production-acceptance.md). Repeat these checks for each release; existing
evidence does not imply that a later source revision or provider account passed.

The repository uses the approved [MIT license](../LICENSE). Publishing, tagging,
committing, pushing, merging and production deployment remain operator actions.
The local candidate tooling performs none of them.

## Source and compatibility gate

Finish the milestone's source, tests, consumers and documentation first. Confirm
the [parity reconciliation](guides/parity-reconciliation.md) accounts for every
master row and the [compatibility policy](compatibility.md) describes any changed
API or persisted/wire format. Keep all module Go requirements aligned with root
[go.mod](../go.mod). Regenerate owned output with the candidate generator, then
format and run the consolidated verification/fix rounds. Do not hand-edit
generated files or discard their ownership manifests.

The final source must pass `make verify`, required PostgreSQL/Redis integration,
relevant race/fuzz/fault suites, all compiler rejection/editor scenarios and the
real TypeScript HTTP/realtime contract gate. Existing project services and unique
non-destructive test namespaces supply integration infrastructure. Record exact
commands, source hashes, tool versions, failures/fixes and final results. A skipped
required backend or editor check does not pass its acceptance requirement.

Review [storage certification](guides/storage.md): the milestone 11 live AWS S3
and Cloudflare R2 evidence covers that accepted source/provider configuration.
The final source gate must either retain justified unchanged adapter evidence or
repeat relevant certification after changes to signing, transport, multipart,
ownership or provider behavior. Never claim a later account/provider check from
historical evidence. Optional real-account email smoke sends remain unverified;
local SMTP/TLS and provider contract fixtures are the recorded email evidence.

## Private packaged consumer gate

[tools/release](../tools/release) is an independent development module using
Go's maintained `golang.org/x/mod` module parser, ZIP validator and module hashing.
It adds no runtime dependency to Foundry-Go. `make verify` includes its tests and
the Python harness tests. Module checksums remain committed alongside its go.mod.

After final generation, prepare a new private output directory:

```sh
cd tools/release
go run . --root ../.. --out ../../.cache/release-candidate-24 \
  --version v0.0.0-candidate.24
```

The example is a synthetic local candidate version, not a chosen public release.
Select a new unused output path for every attempt. The packager refuses existing
destinations, copies rather than edits source modules, and produces:

- Canonical framework, base-plugin and dependent-plugin module ZIPs, `.mod` and
  `.info` records behind a private file module proxy.
- A full independent consumer, a small ordinary consumer and a configured startup
  consumer, including its localization test declarations/assets, using the same
  [profile source](../tests/fixtures/consumer/productionprofile), with replacements
  removed and candidate requirements selected explicitly.
- SHA-256 source/archive inventories and Go `h1:` archive hashes. Generated
  ownership manifests and the root license remain in the packages.

Hidden configuration, caches, credentials files, dependency trees, Git metadata
and nested modules are excluded. Unexpected non-source files and symbolic links
fail packaging. Explicitly review new asset types before extending that policy.
The policy is a file boundary, not a universal secret detector: review selected
source and artifact content as well. A failed preparation may leave an incomplete
private directory; it is not an accepted candidate and is never overwritten.

Run the [native measurements](guides/developer-resource-measurements.md) against
that directory. Downloads and builds use a new module cache, `GOWORK=off`, no
local replacement, the candidate file proxy, and the public Go proxy for third
party dependencies. The root and plugin versions must match the candidate, every
dependency directory must be inside that isolated module cache, `go mod verify`
must pass, and the generated-source hashes must remain reproducible. The ordinary
fixture runs query compilation without starting services; the configured fixture
boots named memory caches and a loopback HTTP kernel, checks a request and drains
its resources. Format-2 manifests identify all three profiles.

The artifacts prove private package consumption without the workspace or Rust
sibling. They do not prove that a public tag exists or that a public proxy has
ingested it. Go module ZIPs intentionally exclude nested test modules, so the
repository gate runs against the checkout; the packaged consumer gate runs
against downloaded candidate modules.

## Dependency and security review

Record the complete selected module graph, Go SDK, gopls build identity, native
platform, and dependency license file hashes. Review root direct dependencies,
transitive provider/client dependencies, tool-only modules, native components and
the TypeScript development lockfile. Check retained license/notice obligations
before distribution; unclassified or missing license files require review.

Use an explicitly selected, existing `govulncheck` binary against the packaged
framework and independent consumer. Retain its JSON configuration and findings;
JSON mode can exit successfully while reporting vulnerabilities. The measurement
harness fails on reported function-level findings and leaves all findings for
review. Investigate reachability and fix applicable findings, then rerun affected
checks and the final gate. An unavailable/incompatible scanner or unreachable
database is an incomplete review, never a clean scan. A scan covers known
database entries at that time, not all possible vulnerabilities.

The scanner follows the [Go vulnerability workflow](https://go.dev/doc/security/vuln/).
No dependency is automatically upgraded or scanner installed by the harness.
Record any approved upgrade, its reason, compatibility impact and validation.

## Final acceptance and handoff

Publish measured conditions and results, explicit omissions, rolling-upgrade
instructions and the [operations runbook](guides/production-operations.md).
Perform one complete framework-wide implementation audit after verification;
batch its fixes, repeat affected checks and complete a final full gate. Only then
mark milestone 24 and the implementation goal complete. Deferred milestone 25
is outside the first release. Preserve evidence and give the operator a concrete
source/artifact result to review before any publication or deployment.
