# Rehearsal conditional multipart certification

## Current contract

The pinned stand-in is
`versity/versitygw:v1.10.0@sha256:89cad13cc981986ee284cbafea4613f9533ce6fd5f37a2c96c4c4fb8adb3a316`.
Its conditional multipart certification is **uncertified**. The owner-reported
container run on 2026-11-15 failed the multipart-listing precondition. The pinned
contract records `failed` and grants no multipart capability.

`S3_CONDITIONAL_MULTIPART` in
[`containers.py`](../../tools/deploy/local/containers.py) records the image,
status, and evidence path. Rehearsal grants multipart conditions only when this
record says `passed` and matches `S3_IMAGE`. The Python regression test rejects a
changed pin without a matching record. A completed record must reference retained
JSON evidence for that image and matrix. A pending or failed record grants no
capability.

R2 substitution mirrors the shipping R2 profile: NFC keys, conditional create and
replace, and no single-request limit. The
[live R2 result](b26-resource-provider-acceptance.md#recorded-live-result-2026-11-15)
certifies the source driver, not VersityGW. A source S3-compatible declaration is
preserved. When multipart conditions lack stand-in certification, the
[rehearsal binding opt-in](../guides/host-capabilities.md#rehearse-locally) permits
only named disks to use a conditional-write cap at `PartBytes`. Evidence marks
that run as reduced fidelity. Large-object application policy validation remains
in force.

## Recorded container result (2026-11-15)

The owner reported a failed run on **linux/arm64** against the image and digest
above. The [retained runner JSON](v036-rehearsal-s3-certification-20261115.json)
confirms the platform, image ID, test binary digest, exit code 1, and failed
parent and size-group cases. It was copied unchanged from the supplied run's
local scratch artifacts. This older artifact contains statuses only; the cause
below comes from the owner's diagnostic rerun and raw XML inspection.

`backend.ListUploads` in `storage/s3/provider_test.go:134-135` returned
`storage_integrity_failed: list` on an empty bucket. The response contained zero
uploads and zero common prefixes, but omitted the requested `EncodingType=url`.
The owner confirmed that a raw XML dump through rclone contained no
`<EncodingType>` element. This fails the strict response check in
`storage/s3/uploads.go`. That check prevents ambiguous key decoding and remains
unchanged.

The matrix stopped before conditional create and replace could be certified.
This result does not establish whether conditional `CompleteMultipartUpload`
works. The stand-in remains **uncertified** because it fails S3 listing
conformance.

Rehearsal of R2 policies above `PartBytes` currently requires the explicit
`allow_single_request_conditions` opt-in and is reported as reduced fidelity.
The opt-in caps conditional writes; it does not make a larger application policy
valid. Production R2 behavior is unaffected. A future stand-in upgrade must pass
the complete certification before the opt-in can be removed.

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
individual test results. Each failed case includes `first_failure`, bounded to
256 characters. The message retains a test source location, a safe precondition
summary when recognized, and an allowlisted framework storage fault. Other text
is withheld. Parent failures inherit the first failing child's diagnostic if the
parent has no earlier diagnostic. A pass requires both size groups, the parent
test, a zero exit code, and no skipped or failed cases. Raw SDK diagnostics,
request URLs, headers, object keys, and credentials are not copied into evidence.
Failures stop acceptance and retain the safe result.
Only the run's disposable resources are removed. To retain failed containers for
inspection, set the existing `FOUNDRY_ACCEPTANCE_KEEP=1` option.

After a run, retain its JSON under `docs/evidence/` and update the constant's
`status` and relative `evidence` path to match the observed result. Keep the exact
image reference. A failed or interrupted run does not authorize multipart
conditions. A changed image requires another certification; do not copy a passing
status from an older digest. No runtime flag can promote a pending result.

## Initial implementation verification

Local results on 2026-11-15, before this fix round:

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
shared matrix code. They do not certify VersityGW. The agent ran no containers;
the failed container result above was supplied by the owner.

## Fix round 1 verification (2026-11-15)

- Focused Python modules `test_s3_certification`, `test_rehearsal`,
  `test_acceptance_wiring`, and `test_host_capabilities_guide`: 77 tests passed.
- `make deploy-tools-check`: 1240 tests, 15 skipped, no errors. The completed
  source passed in one run with local Unix-socket access.
- `make docs-check` and `git diff --check`: passed.

Regression coverage includes the exact refusal and opt-in, the pinned failed
record, first-failure attribution across parent and child result ordering, and
bounded sanitization of URLs, headers, credentials, and unknown diagnostic text.
The strict storage listing check and production behavior are unchanged. No Go
source changed, and the agent ran no containers.
