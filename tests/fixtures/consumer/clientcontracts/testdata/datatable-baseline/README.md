# Datatable pre-cutover contract baseline

These artifacts were emitted by `make datatable-contracts` from a read-only
archive of `f3cfffd71999` (the full commit and artifact hashes are in
[provenance.json](provenance.json)). The archive retains the old datatable
runtime, generated projections and `projection dto=true` annotations.

Only the contract test fixture and its Makefile target were added to the archive.
The fixture registrations match the current gate: `client` preserves the existing
client manifest; `reporting` registers all three existing report tables and the
actual guarded query/download routes. Capturing that second cohort adds coverage
without changing the first cohort's contract. The capture used the public
TypeScript publisher, including its ownership manifests, and made no SQL calls.

The artifacts include manifest and OpenAPI JSON, full and surface TypeScript
entries, shared runtime modules, and optional React/Vue adapters. The gate compares
the artifact name set and every byte; it never updates this baseline. A future
intentional contract change requires a separately reviewed baseline migration.
