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
that run as reduced fidelity. The typed `storage.RehearsalRuntime` mode now
allows larger attachment policies at startup and rejects actual conditional
writes above the cap with `storage.RehearsalLimitExceeded`. Normal disk and
policy limits still apply.

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
The opt-in caps actual conditional writes at runtime while allowing larger
policies at startup. Production R2 behavior is unaffected. A future stand-in upgrade must pass
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


## Runtime cap implementation (v0.37.0)

`storage.Capabilities.ConditionalWriteMode` distinguishes strict startup
validation (the zero value) from `storage.RehearsalRuntime`.
`ValidatePutPolicy` defers only the latter's conditional size restriction.
Attachments are the only startup caller; runtime put, copy, and signed-upload
checks retain the cap. Unknown-size conditional writes also fail before I/O.
The refusal retains `storage.Unchanged` and names reduced-fidelity rehearsal
through attachment error wrapping.

The deploy-generated `cloud.conditional_write_mode` marker is independent of
the process's `FOUNDRY_REHEARSAL_RUN_ID`. Configuration decoding, infrastructure
validation, S3 configuration, and disk construction reject a marker without the
identity. The deploy tool checks the matching disposable host context and
acceptance profile before passing the identity to transient operation units,
application units, sanitized feature dispatch, and console commands. Archives and generic configuration
updates cannot issue the marker, including JSON and `_FILE` overrides.

One startup JSON notice per affected disk records the cap and exceeding declared
attachment maxima. Runtime refusal records contain only disk names, caps, and
counts. The disk API and optional metrics collector expose process totals;
rehearsal evidence and observation reports retain only allowlisted public fields
from a bounded journal window. See the [reporting contract](../guides/host-capabilities.md#rehearse-locally).

Native verification on 2026-11-15:

- `go build` and `go vet` passed for `./storage/...`, `./attachments/...`,
  `./infrastructure`, and `./application`.
- `go test -count=1 -timeout=20m` passed for those nine packages. PostgreSQL,
  Redis, and live-provider tests remained unconfigured and skipped their opt-in
  cases. A subsequent database-free attachment-manager regression and its vet
  check passed. It proves that a 500 MiB policy starts in rehearsal, strict mode
  still refuses it, inspection snapshots are owned, and `MaxObjectBytes` remains
  enforced. The PostgreSQL upload regression is present but was not executed.
- Runtime tests prove both conditional create and replace refusal before reading
  input, unknown-size refusal, at-cap success, unchanged unconditional writes,
  typed unchanged outcomes, and refusal counts. The application startup test
  proves one redacted notice per disk and a redacted runtime refusal record.
- Infrastructure parsing tests prove missing/malformed run identity refusal,
  invalid provider/mode refusal, and that identity alone grants no capability.
  The infrastructure configuration schema and ownership manifest were regenerated.
- Focused Python checks passed: 230 tests in the initial available modules,
  91 tests in the corrected selection, and 86 tests in the final affected rerun.
  The first invocation also named two nonexistent modules; these were invocation
  errors, replaced with the modules that own runtime configuration and dispatch.
- `make deploy-tools-check` ran 1,412 tests with 18 skips. Its sole failure was
  the explicit candidate Python inventory missing `lib/rehearsal_storage.py`.
  The inventory was corrected. The final 86-test rerun includes that check,
  operation launch/cleanup, rehearsal orchestration, certification mapping, and
  marker guards. This is a segmented verification result, not a second full run.
- Source review found and corrected transient systemd operation identity loss.
  A regression proves the guarded identity reaches the operation executor and
  its cleanup through `--setenv`; built-in and custom workload units are covered
  separately. Production defaults emit neither the marker nor the identity.
- Documentation/local-link and whitespace checks passed.

No containers, database services, commits, or live provider certification were
run. The optional container fixture assertion was not added; native startup and
runtime regressions cover the behavior without a container dependency.
