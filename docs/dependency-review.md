# Dependency review

Milestone 24 completed the [recorded candidate review](production-acceptance.md).
The isolated consumer selected 60 dependency modules including framework/plugins,
with no local replacement. Module integrity passed; all modules had license files.
The inventory retained 73 license, notice, copying and patent file hashes, with
MIT/BSD/Apache/ISC-style texts and the separate Go/AOM patent grants classified.

Root [go.mod](../go.mod) and [go.sum](../go.sum) own runtime versions/checksums.
They currently declare 20 direct and 22 indirect module requirements. Keep that
source authoritative rather than maintaining a second version table in prose.
The candidate harness records the selected graph and hashes of its license,
copying, notice and patent files for the actual build.

| Family | Role and review boundary |
| --- | --- |
| AWS SDK/config/credentials/S3 and Smithy | Existing provider transport, signing, credential chain and S3/R2 support. Retain Apache-2.0 license and supplied notices. Review endpoint, retry and credential behavior with adapter changes. |
| pgx/puddle/pgpass/pgservice | PostgreSQL protocol/pool integration. Root credentials stay private; all acceptance uses project-scoped namespaces. Review connection cancellation and transaction ownership. |
| go-redis and its indirect helpers | Redis-backed cache, queues, coordination and realtime. Keep script/atomicity, transport and failover acceptance together. |
| coder/websocket | Native WebSocket transport; its cached license is ISC-style. Preserve transport limits, close ownership and origin policy. |
| x/crypto, x/net, x/sys, x/sync, x/text | Existing cryptographic, network, system, concurrency and text primitives. HTTP connection limits reuse x/net's LimitListener instead of adding another implementation. |
| brotli, nativewebp, gift, gav1d, x/image | Compression/image decoding and transforms. Malformed media, input dimensions, intermediate size, cancellation and codec-specific limits remain the framework boundary. |
| bluemonday/douceur/css | Existing HTML sanitization; retain the established library rather than introducing a second sanitizer. |
| TOML, semver, cron | Configuration parsing, plugin requirements and schedule parsing. Review malformed input bounds and compatibility, not only successful examples. |

All 42 pinned root modules had cached top-level license/copying/notice files in
the source inventory. MIT, BSD-style and Apache-2.0 texts predominate; this
inventory preserves actual files and does not replace their terms. The gav1d
source identifies a pure-Go AV1/AVIF implementation with architecture-specific
assembly. Its COPYING retains dav1d and libaom notices, and PATENTS is separate;
do not omit it from review or assume an unrelated native shared library is used.
The Go SDK and host C toolchain, when selected by ordinary Go builds, are separate
toolchain inputs and are recorded with the measured profile.

## Development-only tools

[tools/release/go.mod](../tools/release/go.mod) isolates x/mod from runtime
consumers. Its patched revision matches the approved vulnerability scanner, and packaging
uses the maintained module parser, canonical ZIP validator and `h1:` hashing.
The module's ordinary tests and vet are included in `make verify`. Run its own
vulnerability check as well as the packaged runtime/consumer checks.

The existing gopls selection is pinned in [tools/gopls.version](../tools/gopls.version).
The TypeScript compiler is development-only and pinned with its lockfile under
[tools/typescript](../tools/typescript/package.json); generated clients have no
runtime npm import. Review the selected scanner/gopls/TypeScript tool identities,
their dependency findings and the lockfile integrity before release. Installing
or upgrading a tool is an explicit verification setup step, not a side effect of
the measurement harness.

## Required final evidence

Follow the [release checklist](release-checklist.md): verify module integrity in
the isolated candidate cache, retain scanner configuration/database timestamps
and every finding, evaluate reachable vulnerable functions, and fix applicable
issues before acceptance. Retain module-only findings for exposure review rather
than calling them function-level reachability evidence. Repeat affected tests
after upgrades and complete the final source gate. Missing scanner execution,
unsupported toolchains or absent database access leave review incomplete.

Root runtime requirements are unchanged. No dependency was installed during
source preparation. During verification, the user explicitly approved installing
govulncheck v1.8.0 into the private milestone tool directory. The release module
now selects x/mod v0.41.0, already obtained with that scanner, after module-only
advisories against its initial older revision. Its ordinary tests/vet and updated vulnerability scan passed without findings.

The packaged framework and independent consumer scans both reported only
[GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932), concerning unmaintained x/crypto
OpenPGP. Neither reported an affected function. Native import graphs (645 framework,
675 consumer packages) contain none of the affected OpenPGP packages. There is no
fixed version for this module-wide advisory; retained x/crypto primitives are
used by auth/encryption, while OpenPGP is outside the implemented surface.

The existing gopls v0.23.0 binary reported module-only
[GO-2026-6179](https://pkg.go.dev/vuln/GO-2026-6179) and
[GO-2026-6180](https://pkg.go.dev/vuln/GO-2026-6180) in x/mod v0.37.0, and
[GO-2026-5970](https://pkg.go.dev/vuln/GO-2026-5970) in x/text v0.38.0.
No affected binary symbols were reported. This is binary-symbol evidence, not
an assertion of source-level reachability analysis. The pinned development tool
was retained and is not a runtime dependency; reassess these findings when
upgrading/rebuilding it. Candidate module downloads use the patched Go 1.27.1
SDK; release packaging uses x/mod v0.41.0. The selected runtime graph has x/text
v0.42.0. No checksum files or dependency trees were deleted as remediation.

All Go scans used govulncheck v1.8.0 with database timestamp
2026-09-15T18:39:25Z. JSON findings were inspected independently of command exit
status. The existing TypeScript lockfile audit reported zero known vulnerabilities.
These results cover known database entries and the recorded native configuration,
not every future toolchain, platform or application import.

The module archives contain framework/plugin source and the approved MIT license;
they do not vendor dependency source or distribute a linked runtime binary.
Dependency module caches retain original license/notice files. A distributor of
linked applications must retain applicable copyright/license notices, AWS/Smithy
notices and gav1d's BSD COPYING plus separate AOM PATENTS terms. Bundled country
and IANA data provenance is recorded in [the data note](../countries/data/README.md);
the Rust-derived country snapshot has no recorded upstream dataset version.

