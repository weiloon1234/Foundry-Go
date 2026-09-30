# E05 — Integrated acceptance and final re-audit

Prerequisites: E01–E04 accepted individually. Status belongs to the
[master](../00-master-architecture-and-parity.md#model-extension-slot-delivery).

## Acceptance matrix

| Area | Required evidence |
| --- | --- |
| Consumer experience | The `articles` consumer reads as ordinary typed Go: slots on the model, one policy method, one assembly line and bound descriptors in constructors |
| Type guarantees | Consolidated compiler-negative cases and generator diagnostics for owner, policy, name and contract misuse |
| Editor and agent support | Real gopls completion and hover on descriptors, slot reads and writes; field notices in model source |
| Loading | Constant query counts per parent batch, nested/paginated/chunked loads, loaded-state distinctions and bounded failure without partial results |
| Transactions | Read-your-writes on the store's pool, parent rollback of text and metadata, attachment publication outcomes after commit |
| Input | Slot-derived validation over JSON and multipart, agreement with write-time checks, strict TypeScript round trips |
| Cleanup | Hard, force and soft deletion, restoration, rollback and duplicate manual cleanup |
| Compatibility | Existing explicit extension APIs, `profiles`, application slices, relations and generated models stay green; generated owners match hand-declared scopes |
| Limits and ownership | Admission, cancellation, overload and shutdown order of managers borrowed by bound descriptors |
| Cost | Repeated list-endpoint measurements at 1, 100 and 1000 rows with and without slots, compared with explicit batch loads; generation and build cost of slot-heavy packages |
| Audit | Complete changed-code review, recorded findings, batched fixes and final-source verification |

## Verification and improvement sequence

Finish the complete source, test and documentation batch before compiling or
testing. Run `make verify`, the affected PostgreSQL integration packages and races,
the consolidated compiler batch, real-gopls smoke, the TypeScript client gate when
client output changed, and deterministic generation. Collect failures, batch
fixes and repeat affected checks until the final source passes.

Then review every change in the series once more. Fix confirmed defects in their
owning package with focused regressions. Remove duplicated logic between bound
descriptors and the existing managers, and verify the final source before updating
the master. Record commands, source identities and measurements in an evidence
record alongside earlier acceptance records. Skipped or unavailable checks are
reported as such.
