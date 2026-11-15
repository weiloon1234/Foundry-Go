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

The security correction replaces the general configuration marker with a typed
`rehearsal` block in the deploy-generated schema-2 runtime descriptor. It contains
`run_id` and exact `{disk, runtime_cap_bytes}` grants. `deployment.LoadRehearsal`
validates a present block's descriptor shape and requires a regular root-owned file with no
group/world write permission before accepting a block. A matching
`FOUNDRY_REHEARSAL_RUN_ID` yields an immutable grant, passed through application
and infrastructure assembly to the S3 adapter and attachment policy validation.
Missing descriptors/blocks or mismatched IDs grant nothing. Present blocks require
a valid schema-2 descriptor. No mutable application-global state is introduced.

The handwritten disk declaration no longer exposes `conditional_write_mode`;
the generated schema rejects that retired key even with an operator-set run ID.
The deploy writer accepts blocks only from its private disposable-host context,
with the matching testing acceptance profile, and preserves the block in console
descriptors. Production, archive, and generic configuration paths cannot issue
one. The existing guarded identity propagation to units and console commands
remains in place.

One startup JSON notice per affected disk records the cap and exceeding declared
attachment maxima. Runtime refusal records contain only disk names, caps, and
counts. The disk API and optional metrics collector expose process totals;
rehearsal evidence and observation reports retain only allowlisted public fields
from a bounded journal window. See the [reporting contract](../guides/host-capabilities.md#rehearse-locally).

Initial implementation verification on 2026-11-15 (before the security correction):

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


## Descriptor gate security correction (round 1)

Regression sources cover the retired config key with an operator-set ID, no
descriptor, no block, absent/mismatched IDs, unlisted disks, invalid ownership,
malformed grants, and the full descriptor-to-storage/attachments assembly path.
The 500 MiB startup regression is retained, including oversized typed refusal
before provider I/O. Native positive reader tests model root ownership at the
private filesystem boundary; actual operator-owned files are refused by the
public reader. Python coverage follows rehearsal mapping through descriptor
publication and console derivation, and refuses publication outside rehearsal.

Native verification on 2026-11-15 used the existing warm Go cache and no
containers. The package scope was `./storage/... ./attachments/...
./infrastructure ./application ./deployment ./internal/deployrehearsal`.

- `GOFLAGS=-trimpath go run ./cmd/foundry generate --recursive --dir infrastructure`
  regenerated the disk schema and ownership manifest. The first attempt caught
  a missing storage import; it was corrected before the successful generation.
- `GOFLAGS=-trimpath go build` and `GOFLAGS=-trimpath go vet` passed for that scope.
- `GOFLAGS=-trimpath go test -count=1 -timeout=20m` passed for all ten test-bearing
  packages; the internal grant package has no standalone tests and is exercised
  by its consumers. The run loaded the existing private `.env.test` from
  `/Users/weiloon/Projects/rust-go/Foundry-Go` without recording its contents,
  with `FOUNDRY_TEST_POSTGRES_REQUIRED=1`, `FOUNDRY_TEST_REDIS_REQUIRED=1`, and
  `FOUNDRY_TEST_REDIS_ADDR=127.0.0.1:6379`. The PostgreSQL attachment startup/upload
  regression executed successfully. Live storage-provider tests remain opt-in.
- `make generate-check` passed for every Makefile-declared generation directory,
  the HTTP-path export, and rendered documentation.
- Focused Python modules `test_rehearsal_storage`, `test_feature_preview`,
  `test_s3_certification`, and `test_rehearsal` ran 96 tests. One new fixture used
  macOS's symlinked temporary path; resolving that path fixed the failure, and
  the targeted regression then passed. No runtime security check was weakened.
- `make deploy-tools-check` completed 1,415 tests with 18 skips. Its only failure
  was the existing
  `test_readiness.ReadinessTests.test_sixteen_slow_probes_cannot_queue_112_short_deadlines`
  timing assertion. That exact test passed on an isolated rerun with unchanged
  source. This is a segmented result, not a clean initial full-suite pass.
  Opt-in live Cloudflare/systemd and Linux-only socket cases remain skipped.
- Final review strengthened the reader regressions to exercise the actual process
  environment lookup while substituting only the filesystem ownership predicate.
  Build, vet, and `go test -count=1 -timeout=20m ./deployment ./application` then
  passed again with the same required database and Redis configuration.
- `make docs-check` and `git diff --check` passed.

Source baseline: `8700ea28`. The final touched-source fingerprint covers 28
Go/Python/generated-ownership files, sorted by relative path, hashing each path,
NUL, contents, and NUL with SHA-256:
`1c9f5801a2f6220c11cb069f71655b8a78e5caaa7db39a8a8fa6c8fc46134df6`.
Documentation is excluded from this fingerprint.

No containers, commits, or live storage-provider certification were run. Existing
PostgreSQL and Redis services were used; no services were started or reset.


## Optional grant loading compatibility correction (round 2)

`application.New` now calls `deployment.LoadRehearsal` instead of the feature-only
`LoadBindings`. No selected descriptor or no rehearsal block returns an empty
grant with `present=false`, without adding a descriptor schema requirement to
application assembly. This preserves schema-1 legacy rollback, schema-2 without
a block, and local application startup. The explicit feature binding API remains
strict. Invalid selected file paths or malformed JSON still refuse rather than
silently falling back.

A present block reuses the complete schema-2 decoder, ownership validation, and
run-ID matching. Explicit null and malformed blocks return `fault.Invalid`.
Both loaders share the bounded canonical read and capture ownership and bytes
from the same open file, avoiding a second read between checking and granting.

Regressions exercise application build/start and strict disk capabilities with
and without a process run ID for no descriptor, schema 1, schema 2 without a
block, and an otherwise schema-free document. The loader regressions retain
matching/mismatched identity, root ownership, malformed grants, exact disk caps,
and the 500 MiB attachment startup/runtime-refusal assertions. Python coverage
checks schema-1 unit and console descriptor generation and the descriptor
actually selected by rollback `config:check`.

Verification on 2026-11-15, from baseline `fd47a51c`:

- `GOFLAGS=-trimpath go build ./deployment ./application` passed.
- `GOFLAGS=-trimpath go vet ./deployment ./application` passed.
- `GOFLAGS=-trimpath go test -count=1 -timeout=20m ./deployment ./application`
  passed. The run loaded `/Users/weiloon/Projects/rust-go/Foundry-Go/.env.test`
  privately, with `FOUNDRY_TEST_POSTGRES_REQUIRED=1`,
  `FOUNDRY_TEST_REDIS_REQUIRED=1`, and `FOUNDRY_TEST_REDIS_ADDR=127.0.0.1:6379`.
- `PYTHONDONTWRITEBYTECODE=1 PYTHONPATH=tools/deploy/lib:tools/deploy/local:tools/deploy/tests
  python3 -m unittest test_feature_preview` passed all 24 tests.

- `make docs-check` and `git diff --check` passed.

- `make deploy-tools-check` passed: 1,416 tests in 228.145 seconds, 18 skips,
  no failures. Skips are the existing opt-in live Cloudflare/systemd and
  Linux-only socket cases.

Touched-source SHA-256 (6 Go/Python files, sorted relative path, NUL, contents,
NUL; documentation excluded):
`3b923371109fe2e68557ce675c1fa9542a95996ed984e0a61d4c6fbbb336ffe4`.
No containers, commits, new services, or service resets were used.


## Dormant rehearsal loading correction (round 3)

`LoadRehearsal` now checks the process identity before looking up or opening the
descriptor. An absent or empty identity returns no grant immediately, including
when the selected descriptor path is empty, missing, unreadable, or contains
malformed JSON. `application.New` has no other descriptor reader.

With a nonempty identity, an unset/empty descriptor path, nonexistent file,
absent block, or mismatched run ID retains strict capabilities. Each successful
load without a grant emits exactly one fixed-text JSON notice with event
`foundry_rehearsal_storage_no_grant`; no path, run ID, credentials, or descriptor
contents are logged. Other read errors, malformed JSON, and malformed blocks
remain typed refusals. The active path validates descriptor shape even without
a block, reusing the existing legacy decoder for schema 1 and feature decoder
for schema 2. Matching grants retain ownership and shape validation.

Regression coverage includes dormant application build/start for missing, empty,
malformed, and unreadable descriptors; silence when dormant; one redacted notice
for active missing descriptors/blocks; active malformed data refusal; and the
existing valid/mismatched grant and 500 MiB policy tests. Synthetic storage-log
tests inject their grant without claiming an active process identity.

Verification on 2026-11-15 from baseline `0e599a85`:

- `GOFLAGS=-trimpath go build ./deployment ./application` passed.
- `GOFLAGS=-trimpath go vet ./deployment ./application` passed.
- `GOFLAGS=-trimpath go test -count=1 -timeout=20m ./deployment ./application`
  passed. The run loaded `/Users/weiloon/Projects/rust-go/Foundry-Go/.env.test`
  without printing its contents, with `FOUNDRY_TEST_POSTGRES_REQUIRED=1`,
  `FOUNDRY_TEST_REDIS_REQUIRED=1`, and `FOUNDRY_TEST_REDIS_ADDR=127.0.0.1:6379`.
- Source review found no other descriptor reader in `application.New`.

- Final review added schema validation to the active no-block path using the
  shared legacy decoder. The same build, vet, and required-backend Go tests
  passed again for the final source.
- `make deploy-tools-check` passed: 1,416 tests in 224.451 seconds, 18 skips,
  no failures. Existing opt-in live Cloudflare/systemd and Linux-only socket
  cases remain skipped.
- Final `make docs-check` and `git diff --check` passed.

Touched-source SHA-256 (4 Go files, sorted relative path, NUL, contents,
NUL; documentation excluded):
`20352aa6105f7c094dda829622b4a599027e1f4e61f930bb9a6b85350cf2cd7d`.
No containers, commits, new services, or service resets were used.
