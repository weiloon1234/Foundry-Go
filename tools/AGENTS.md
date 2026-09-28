# Verification and release tooling

These instructions add to the [repository rules](../AGENTS.md). Command ownership
stays in [Makefile](../Makefile); use the [release checklist](../docs/release-checklist.md)
only when the task includes release/package acceptance.

## Native checks and cache costs

- Read SDK requirements from root `go.mod`; use native macOS executables and existing
  approved tool selections. Align development/fixture modules without changing pins
  or installing a different tool as an implicit verification side effect.
- Preserve package batching and external-input fingerprints. Do not force all tests
  uncached or replace the shared cache with a fresh cache for every child compile.
- Bound child processes, output and time; retain their process group until exit.
  An observation timeout is not proof of termination. Inspect/poll an existing live
  run before starting another full check.
- Measure cold builds only in explicit performance/package work. Keep generation,
  build and editor caches distinct as the harness specifies, run profiles sequentially,
  and retain toolchain, source fingerprints and actual resource conditions.
- Check free disk before expensive runs. After recording evidence and confirming
  their processes exited, remove only task-owned temporary measurement caches.
  Keep source, reports, logs, archives and notices. Do not delete shared caches during
  live work or delete module/checksum files as a dependency fix.

## Private packages and security evidence

- Release tools must remain independent of unreleased runtime imports. Use maintained
  module/ZIP/hash utilities and the existing source allowlist. Hidden/private files,
  credentials, dependency trees, symlinks and nested modules stay excluded.
- Keep generated ownership manifests and licenses in eligible packages. Any new asset
  type needs a deliberate source-policy review; do not broaden inclusion to make a
  packaging failure disappear.
- Use a fresh private candidate destination. Independent consumers must resolve
  intended candidate versions without local replacements and reproduce generation.
- Inspect vulnerability JSON findings as well as exit status. Record database/tool
  versions and distinguish module/package/function reachability. Missing scanner or
  backend evidence is incomplete; retain license/notice obligations and scan limits.
- Keep credentials out of command output, logs and evidence. Private test configuration
  is an input, never an artifact to copy into a candidate.
- Packaging and measurement do not authorize tags, publishing, deployment or Git writes.

Use existing release/measurement tests when changing these tools. A documentation
or instruction edit needs link/factual checks, not a fresh packaged benchmark run.
