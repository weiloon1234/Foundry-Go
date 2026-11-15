# Rehearsal conditional multipart certification

## Current contract

The pinned stand-in is
`versity/versitygw:v1.10.0@sha256:89cad13cc981986ee284cbafea4613f9533ce6fd5f37a2c96c4c4fb8adb3a316`.
Its conditional multipart certification is **pending**. No container run was
performed for this change, and no passing provider result is claimed.

`S3_CONDITIONAL_MULTIPART` in
[`containers.py`](../../tools/deploy/local/containers.py) records the image,
status, and evidence path. Rehearsal grants multipart conditions only when this
record says `passed` and matches `S3_IMAGE`. The Python regression test rejects a
changed pin without a matching record. A completed record must reference retained
JSON evidence for that image and matrix. A pending record grants no capability.

R2 substitution mirrors the shipping R2 profile: NFC keys, conditional create and
replace, and no single-request limit. The
[live R2 result](b26-resource-provider-acceptance.md#recorded-live-result-2026-11-15)
certifies the source driver, not VersityGW. A source S3-compatible declaration is
preserved. When multipart conditions lack stand-in certification, the
[rehearsal binding opt-in](../guides/host-capabilities.md#rehearse-locally) permits
only named disks to use a conditional-write cap at `PartBytes`. Evidence marks
that run as reduced fidelity. Large-object application policy validation remains
in force.

## Run the stand-in certification

From the framework root on an arm64 Docker host, run:

```sh
ACCEPTANCE_LOG_DIR="$(mktemp -d)/s3-certification" \
  bash tools/deploy/tests/run_acceptance.sh \
  --ubuntu 24.04 --platform linux/arm64 --framework-root "$PWD" \
  --fresh --s3-certification-only
```

Use `linux/amd64` on an amd64 host. No application archive is required. The runner
uses the existing acceptance S3 container, host container, CA, isolated network,
and cleanup. It publishes no host ports. Normal container acceptance also runs
this phase before application phases.

The native Go toolchain cross-compiles `./storage/s3` into a Linux test binary.
The host runs `TestRealCompatibleConditionalMultipart` over HTTPS with explicit
`FOUNDRY_TEST_COMPATIBLE_*` endpoint, bucket, prefix, region, and credential
variables. `FOUNDRY_TEST_COMPATIBLE_REQUIRED=1` makes missing configuration fail.
The Compatible entry point shares `runRealConditionalMultipart` and
`runConditionalMatrix` with `TestRealR2ConditionalMultipart`; there is no second
matrix. It tests both 5242880 and 5242881 bytes, covering single-request and
multipart create and replace, competing writes, changed destinations, lost
acknowledgements, cancellation, failed parts, and abort outcomes.

The runner writes `s3-certification.json` in the printed log directory. It records
the pinned image, resolved image ID, platform, test binary digest, exit code, and
individual test results. A pass requires both size groups, the parent test, a zero
exit code, and no skipped or failed cases. Raw SDK diagnostics and credentials are
not copied into evidence. Failures stop acceptance and retain the safe result.
Only the run's disposable resources are removed. To retain failed containers for
inspection, set the existing `FOUNDRY_ACCEPTANCE_KEEP=1` option.

After a run, retain its JSON under `docs/evidence/` and update the constant's
`status` and relative `evidence` path to match the observed result. Keep the exact
image reference. A failed or interrupted run does not authorize multipart
conditions. A changed image requires another certification; do not copy a passing
status from an older digest. No runtime flag can promote a pending result.

## Local verification

Local results on 2026-11-15:

- Focused Python modules `test_rehearsal`, `test_s3_certification`,
  `test_acceptance_wiring`, and `test_host_capabilities_guide`: 74 tests passed.
- `make deploy-tools-check`: 1237 tests, 15 skipped, completed as a segmented run.
  The first run had five sandbox Unix-socket errors and one tool-bundle mismatch
  while a source edit changed its fingerprint. All six affected tests passed on
  the unchanged final source with Unix-socket access. The full suite was not
  restarted.
- `go build ./storage/s3`, `go vet ./storage/s3`, and `go test ./storage/s3`:
  passed using the native toolchain and existing cache. Live provider tests remain
  opt-in and were not configured.
- `make docs-check`, shell syntax for `run_acceptance.sh`, and
  `git diff --check`: passed.

These checks validate mapping, refusal, opt-in bounds, certification wiring, and
shared matrix code. They do not certify VersityGW. No containers were run.
