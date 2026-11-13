# B26 resource and provider acceptance

Status: acceptance sources added; container, resource-matrix and live-provider
results are not certified by this document. The core and ingress lane's earlier
passes do not verify these new sources. No production R2 capability is widened.

## Resource matrix

`attachments/resource_acceptance_test.go` runs each path, size and concurrency in
its own test process. Inputs are generated on demand. Storage is the real local
adapter, transactions use isolated PostgreSQL schemas, and URL import uses a
separate streaming peer process. The peer's address is restricted to its loopback
port. No input-sized source or response recorder is used.

The cells exercise public Prepare/ReplaceIn, HTTP multipart capture through
publication, slot Accepts with accepted and rejected media, AddFromURL, full Open,
64 KiB of seeks/ranges, checksum-fallback ReconcilePending, explicit Settle with
checksum metadata, and historical variant rejection. Near-64-MiB Prepare,
Accepts and URL cells run at 64 MiB minus one, exactly 64 MiB, and plus one under
a 512 MiB policy. Historical rejection is followed by streaming and retirement.
Additional measured cells cover rejected/oversized/failed/canceled URL input,
canceled Accepts, corrupt/truncated full reads, and mismatching/canceled fallback
and explicit settlement. Uncertain rows and their objects must survive incomplete
evidence. Existing preparation and PostgreSQL tests retain policy-change and
transaction-outcome coverage.

Every streaming path compares 128 MiB and 512 MiB at fixed concurrency 1 and 2.
Each concurrent worker has an isolated store and manager with one read and one
mutation slot. These cells measure aggregate overlapping operations; they do not
claim a deployment-wide manager limit. The report includes runtime version,
platform, GOMAXPROCS, GOGC/GOMEMLIMIT overrides, baseline and peak HeapAlloc/RSS,
cumulative allocation, request/attachment/local-staging logical byte high-water
marks, actual source/storage payload counts, and the expected SHA-256. Output
reads verify exact length and checksum. Sampling uses 10 ms intervals, with RSS
every fifth sample. Cumulative allocation guards catch brief large allocations
that the samples could miss. RSS is reported separately, not treated as Go heap.
The local adapter has no multipart parts; the report records zero part size and
concurrency. The separate S3 wire and live tests own multipart evidence.

Limits are 96 MiB incremental peak heap and cumulative allocation per operation,
and at most 16 MiB additional heap/allocation per operation from 128 to 512 MiB.
A bounded complete-detection buffer at 64 MiB must fit the same envelope. Disk
bounds allow one payload per preparation or local staging operation, plus framing
or local metadata, and one request capture. Post-operation checks require empty
preparation spools, released request reservations/readers, and no local staging.
These limits are acceptance requirements, not measured results.

Run on a host with the existing isolated PostgreSQL test configuration and enough
private temporary disk for concurrent spools and local objects. Do not run the
resource matrix with `-race`; use race tests separately. From the repository root:

```sh
FOUNDRY_TEST_POSTGRES_REQUIRED=1 FOUNDRY_TEST_ATTACHMENT_RESOURCES=1 \
  go test -timeout=90m -count=1 -v ./attachments \
  -run '^TestB26ResourceAcceptance$'
FOUNDRY_TEST_POSTGRES_REQUIRED=1 go test -race -timeout=20m ./attachments ./storage/s3 \
  -run 'TestPostgres(HistoricalLarge|Large|R2)|TestRecoveryRejectsIncomplete|TestFullStreamingReadRequiresCompleteDigest|TestLargeConditionalMultipart|TestR2ConditionalPublicationCandidateWireMatrix'
```

Retain the complete command output, source revision and diff, toolchain, free
space, machine load, and filesystem/volume topology with `B26_RESOURCE` records.
Do not relax a failed budget without explaining the allocation or retained owner.
A skipped database or missing cell is incomplete evidence.

## Rendered nginx and quota scenarios

`tools/deploy/tests/acceptance_uploads.py` runs after the ordinary edge phase in
`run_acceptance.sh`. It uses the installed deploy-acceptance fixture and real
production nginx/systemd renderers, a real attachment manager and local storage,
and PostgreSQL commits. The adapter wrapper holds publication inside Put for
150 seconds without writing response bytes. It records each Put attempt and
checks exact stored digest/length and one ready database row after success.

The controller sends known-length and chunked bodies from a fixed 64 KiB block.
It samples all five volumes, rejects proxy high-water occupancy of 1 MiB or more,
and exercises progressing, stalled, trickling and disconnected clients. The short
route declares six seconds total; nginx declares three seconds of client-body
inactivity. Other long routes retain the actual 150-second silent publication.
Small JSON and small multipart attachment requests run during long operations.

Proxy, request, attachment and staging saturation are separate scenarios. Direct
requests bypass nginx for application-owner checks. Proxy slots are fewer than
request slots, so proxy exhaustion cannot be attributed to the application pool.
Each large volume is also filled with a test-owned file until ENOSPC/EDQUOT, then
that exact file is removed and a new publication must succeed. The small volume
is never filled. The controller restores the original unit, nginx site and HTTP
service and checks the unchanged registry. Credentials stay in a private temporary
environment copy and are never printed. These files and mounts exist only inside
the guarded disposable container. Sparse backing images are fault-injection
fixtures, not production capacity-reservation evidence.

The fixture passes the declared sublimits to `attachments.Config.MaxLargeActive`
and `local.Config.Staging`; neither gate lives in the fixture. Large attachment
policies acquire their sublimit before shared mutation capacity and before source
work. Local puts and copies reserve large-write slots and bytes before staging;
unknown lengths reserve the adapter maximum. Failed cleanup retains its byte
charge until prune succeeds, and restart accounts for interrupted staging files.
Known small writes retain independent admission. The small collection uses its
own managed root on the small volume, preserving disk space during large-volume
exhaustion. Local staging remains inside each managed root for atomic rename;
published data and record overhead require capacity beyond payload reservations.

Attachment slots exceed staging slots in this profile, so the local saturation
scenario reaches the adapter's gate rather than stopping at the manager. Go
regressions cover these owners and configuration propagation, but have not been
run for this revision. Resource, container and live-provider certification remain
lead-run evidence; implementation and controller unit checks do not certify them.

Build and run each supported OS/architecture cell separately. These commands
require Docker and network access and belong to the lead:

```sh
framework="$PWD"
build_info=$(mktemp -d)
python3 tools/deploy/tests/build_acceptance.py --architecture arm64 \
  --archive-file "$build_info/archive.txt"
archive=$(cat "$build_info/archive.txt")
ACCEPTANCE_LOG_DIR="$build_info/ubuntu-24-fresh" \
  bash tools/deploy/tests/run_acceptance.sh --ubuntu 24.04 --platform linux/arm64 \
  --archive "$archive" --framework-root "$framework" --fresh
ACCEPTANCE_LOG_DIR="$build_info/ubuntu-26-foreign" \
  bash tools/deploy/tests/run_acceptance.sh --ubuntu 26.04 --platform linux/arm64 \
  --archive "$archive" --framework-root "$framework" --foreign
```

The existing amd64 CI runner owns baseline resolution, archive construction and
upgrade flags. Run all four cells with fresh log directories:

```sh
matrix_logs=$(mktemp -d)
for ubuntu in 24.04 26.04; do
  for mode in fresh foreign-upgrade; do
    ACCEPTANCE_LOG_DIR="$matrix_logs/$ubuntu-$mode" \
      bash tools/deploy/tests/run_ci_acceptance.sh "$ubuntu" "$mode"
  done
done
```

The upgrade uses the published generation-9 baseline selected in that runner,
not a modified checkout. `uploads.log` retains per-scenario results and
`UPLOAD_DISK` records.
All cells must pass; rendering/unit tests cannot replace these observations.

## Live R2 matrix

Use an existing test-only bucket and prefix ending in `/`. Supply
`FOUNDRY_TEST_R2_BUCKET`, `FOUNDRY_TEST_R2_PREFIX`, `FOUNDRY_TEST_R2_ENDPOINT`,
`FOUNDRY_TEST_R2_ACCESS_KEY_ID` and `FOUNDRY_TEST_R2_SECRET_ACCESS_KEY` through the
existing private environment. Do not paste credentials into commands or reports.
The tests generate random descendants and clean only their exact objects/uploads.

```sh
FOUNDRY_TEST_R2_REQUIRED=1 go test -timeout=30m -count=1 -v ./storage/s3 \
  -run '^(TestRealR2StorageContract|TestRealR2ConditionalMultipartCandidate)$'
```

The candidate matrix covers absent/existing and competing creates, matching/stale/
missing-destination and competing replacements, destination mutation before
completion, winner bytes, cancellation/abort, ambiguous completion and cleanup.
It tests both single-request and multipart thresholds. The candidate is private
to the test. Shipping R2 retains `ConditionalWriteMaxBytes` until live evidence
certifies both create and replace. A passing local TLS S3 stand-in is not R2
certification and does not authorize removal of that restriction.

## Independent consumers and fast checks

The existing plugin, consumer and Go-way fixtures are unchanged. The consumer additions are the deploy fixture's opt-in upload routes, generated
receipt DTOs, and regeneration of its stale configuration schema to include
`attachments.config.temp_directory`.
Run the existing fixture gate at the lead checkpoint:

```sh
make fixture-check
```

Fast checks and their actual outcomes are recorded in the task report. Container,
resource and provider commands above are separate gates, not implied passes.

## Local implementation checks (2026-11-14)

No container, SSH, host provisioning, PostgreSQL or live-provider command was
executed for this slice. Native Go commands used `GOPROXY=off` and
`GOFLAGS=-trimpath`; cache access required sandbox escalation, which was granted.
No dependency installation or Git commit/push/merge was performed.

| Command and working directory | Result |
| --- | --- |
| Root: `go run ./cmd/foundry generate --recursive --dir tests/fixtures/deploy-acceptance` | Generated two new DTOs and refreshed the fixture configuration schema and ownership manifests |
| Root: `go test ./attachments -run '^(TestB26ResourceAcceptance\|TestB26ResourceCell\|TestB26ResourcePeer\|TestLargePoliciesRetainImageBounds\|TestHistoricalOriginalVariantGuardPrecedesStorage\|TestLargePreparationLimitsAndFailureCleanup)$'` | Passed; resource subprocess tests deliberately skip without their opt-in environment |
| Root: `go vet ./attachments` | Passed |
| `tests/fixtures/deploy-acceptance`: `go test ./...` and `go vet ./...` | Passed; subsequent handler changes checked with `go test ./internal/app -run '^TestUpload'` and vet |
| `tests/fixtures/consumer`: `FOUNDRY_TEST_POSTGRES_URL= FOUNDRY_TEST_POSTGRES_REQUIRED=0 go test ./httpuploads ./articles ./profiles` | Passed, PostgreSQL explicitly disabled; source unchanged |
| Root: `make deploy-tools-check` | 625 tests, nine opt-in skips, one sampler-test race failure; corrected and rerun below |
| `tools/deploy/tests`: `PYTHONDONTWRITEBYTECODE=1 python3 -m unittest test_acceptance_uploads test_acceptance_wiring` | All 13 affected tests passed after correction; unaffected results reused |
| Root: `make docs-check generate-check` | Passed |
| Root: `go run ./cmd/foundry generate --recursive --check --dir tests/fixtures/deploy-acceptance` | Passed; this nested module is outside the root generation list |
| Root: `bash -n tools/deploy/tests/run_acceptance.sh` and `git diff --check` | Passed |

The first attachment compile also caught a missing `ClientContentType` method in
the generated input source; the corrected source passed the targeted test and vet.
These checks establish source/contract consistency and local barrier behavior.
They do not supply measured resource, full consumer-gate, container or R2 results.
The master milestone status is unchanged.
