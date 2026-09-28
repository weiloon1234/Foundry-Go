# Production acceptance and framework audit

Milestones 01–24 cover the agreed first-release scope; milestone 25 remains deferred.
The [master status](../blueprint/00-master-architecture-and-parity.md) is authoritative. Publication, version tagging and deployment are
separate operator actions. This report records local candidate evidence only.

## Verification

The complete milestone source/test/documentation batch preceded compilation.
Initial consolidated verification exposed generator fixture names, cleanup
expectations, callback snapshot equality and stale independent plugin sums. Those
were corrected together and the complete pre-audit gate passed in 506.5 seconds.
The gate includes native PostgreSQL and Redis, all root and independent fixture
packages, vet/formatting, generation freshness, documentation, strict TypeScript
and real HTTP/WebSocket interoperability. Its catalog has 934 negative compiler
cases, 380 consumer gopls scenarios, three basic probes and six field-documentation
scenarios. Catalog counts describe executable cases, not distinct API methods.

Selected runtime/database/consumer races passed in 145.5 seconds across five
package groups. Bounded trace fuzzing passed 3,993,247 inputs in 20.48 seconds.
These fault/load/fuzz results establish tested bounds, not throughput guarantees.
The blocked-exporter load test accounts for 2,048 attempts from 32 workers with
bounded spans, queues and metrics. Native HTTP tests verify that the connection
cap holds before handler admission.

## Complete-framework audit round

Every family in the [parity reconciliation](guides/parity-reconciliation.md) was checked against source, consumer
contracts and acceptance coverage. Source review concentrated on shared ownership,
authorization, transaction/executor provenance, bounded resources, generated/wire
identity and recovery. This is a framework-wide risk-focused audit, not a claim
that every line was reread or all possible faults were exhausted.

The audit corrected:

- Error classification for storage, database, worker and pub/sub now retains
  actual owners through arbitrary error-method callbacks and contains panic/Goexit.
  Failed database acquisition retains its owner until classification returns.
- Worker reserve/start/renew/finish callbacks remain owned through actual exit;
  ambiguous writes still require reconciliation instead of automatic replay.
- Application lifecycle cancellation inspection runs outside its mutex, and
  completion joins errors without invoking unnecessary error methods. Lease and
  transaction terminal sentinels use their exact known identities.
- Route factories must preserve method/path/access, concrete Go transport types
  and required signing. Runtime codec/validation configuration remains owned by
  the actual returned descriptor rather than guessed structural equivalence.
- Generated field-documentation rollback keeps immutable new-source proof so a
  second interruption can recover without expanding ownership to handwritten data.

Grouped runtime races passed in 17.8 seconds, repeated recovery races in 13.6
seconds and the final database follow-through races in 8.1 seconds. An initial
owner-count assertion was corrected to check retained ownership rather than a
private count. Database drivers must obey database/sql's driver contract, including
error methods that database/sql itself invokes; framework callback isolation does
not repair internal driver/stdlib state after a driver violates that contract.

## Evidence boundaries

All 58 storage baseline files were compared with milestone 11: the only changed
implementation is the shared disk outcome-classification boundary, covered by new
native/race cases. S3/R2 signing, transport, multipart and provider source is
unchanged. Recorded live AWS S3/R2 certification is retained for that source and
configuration; this is not a new live-account certification. The AWS public-URL
case was optional on a private bucket. R2 conditional deletion is explicitly
unsupported. Temporary provider credentials were removed after certification.

Optional real-account email sends remain unverified; local SMTP/TLS and provider
contract fixtures passed. Replica tests prove explicit routing and failures, not
replication lag or immediate visibility. The country snapshot retains the supplied
Rust source/hash and has no recorded upstream dataset version; IANA's bundled
2026a table retains its public-domain notice.

The approved root MIT license matches Rust Foundry. Dependency licenses and
notices belong to their respective modules. Source-module ZIPs do not vendor
third-party code; distributors of linked applications must retain applicable
license/notice/patent texts, including gav1d's separate AOM PATENTS file.

## Post-audit and independent candidate results

The complete native post-audit `make verify` gate passed in 739.5 seconds against
3,536 frozen source inputs; all hashes still matched afterward. The independently
packaged runtime code is the same audited code. Final acceptance documentation
adds the recorded results; final artifact comparison checks that those additions
change no runtime, generator, consumer or plugin source.

Private candidate `v0.0.0-candidate.24` contains three canonical module ZIPs:
framework, base plugin and dependent plugin. The measured root archive contains
2,012 files and each plugin seven files, including the approved MIT license.
Source and archive hashes, canonical Go module hashes and private proxy metadata
are retained in each candidate's `manifest.json`. This synthetic version does
not name a published release. The final artifact inventory may additionally
contain these acceptance documents.

Fresh isolated module caches consumed all three candidates without local
replacements or sibling Rust files. The selected graph contains 60 dependencies
(including the framework/plugins), all with license files; `go mod verify`
passed. Ordinary and full consumers reproduced owned generated output exactly,
built with default compiler flags, compiled again after one actual model edit,
and passed three real gopls completion/hover rounds. The small linked application
compiled typed query ASTs successfully without contacting services.

On the recorded 64 GiB, 16-core native Mac, ordinary/full cold generation took
4.34/61.43s, cold builds 3.30/60.24s and edited builds 1.19/1.44s. The broad profile
sampled about 12.7 GiB across its process tree, versus under 0.7 GiB for ordinary
work. [Measurements and operating budgets](guides/developer-resource-measurements.md)
include raw phases, generated size, editor timing, hardware and sampling limits.
Cold/incremental measurements are single samples; they are not production
throughput or continuously running editor latency claims.

The explicitly approved govulncheck v1.8.0 scanned the packaged framework and
consumer with vulnerability database timestamp 2026-09-15T18:39:25Z. Neither scan
reported an affected function. Both retained the module-only GO-2026-5932 finding:
unmaintained OpenPGP packages in x/crypto. None occurs in the actual native
framework or consumer import graph. The release tool's patched x/mod v0.41.0
scan has no findings. [Dependency review](dependency-review.md) retains the
separate pinned-gopls module-only advisories, zero-vulnerability TypeScript
lockfile audit and all license/notice/patent obligations.

Actual archives were checked for excluded paths, configured private database
URL/password material and common private-key/token patterns. Verification logs
were checked for the configured database credentials without printing them.
Those checks found no matches; they are scoped evidence, not a universal secret
proof. No Git commit/push/merge, external email, publication or production
deployment occurred during this acceptance work.

Private working evidence is retained under `.cache/milestone24-production/`.
The controlled candidate and raw measurements are under
`.cache/release-candidate-24-measured/`; final source artifacts are under
`.cache/release-candidate-24-final/`. These paths are local evidence, not public
release downloads. The public [compact measurement record](evidence/production24-native.json)
omits machine paths, credentials and raw scanner advisory text.

Use the [release checklist](release-checklist.md), [compatibility policy](compatibility.md)
and [operations runbook](guides/production-operations.md) for subsequent releases.
Milestone 25 remains deferred and the evidence limits above remain explicit.
