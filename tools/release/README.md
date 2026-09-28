# Private release preparation

This independent development module prepares canonical module-proxy artifacts
with Go's maintained `golang.org/x/mod` library. It adds no dependency to framework
consumers and performs no publishing or version-control operations.

Run it only after source generation and milestone verification. Follow the
[release checklist](../../docs/release-checklist.md) and
[measurement procedure](../../docs/guides/developer-resource-measurements.md).
Root go.mod owns the Go requirement; `make docs-check` keeps this module aligned.
The dependency version uses the patched x/mod revision already selected by the
approved vulnerability scanner. Checksums are retained in go.sum.

`make release-tools-check` from the repository root runs packaging and harness
tests; the target is part of `make verify`. Preparation requires a new private
output directory and explicit canonical candidate version. Existing output is
never overwritten. A failed directory is retained as incomplete evidence.

Format-2 candidates include ordinary, configured and full consumers. The configured
profile is copied from `tests/fixtures/consumer/configuredprofile` alongside the
existing typed `productionprofile` model. The measurement harness accepts earlier
format-1 candidates separately; profile source inventories must still match exactly.
