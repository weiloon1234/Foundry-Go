# 16 — Email

## Purpose and prerequisites

Prerequisites: [11](11-storage-reliability.md), [12](12-jobs-and-worker-kernel.md). Reproduce Foundry's email facilities through a typed message API and replaceable transports.

Rust references: `src/email`, `src/email/job.rs`, `docs/guides/email-and-notifications.md`, `tests/acceptance.rs`.

## Public contracts

Email owns validated addresses, recipients, message/template data, attachment references, transport results and driver interfaces. Templates receive declared Go data structs; arbitrary maps are an explicit dynamic option. Use standard safe HTML template escaping.

Planned call shape:

```go
err := mailer.Send(ctx, WelcomeEmail{Recipient: address, Name: name})
```

Queued messages serialize their typed data and stable attachment references, not open readers or request temporary-file paths. Storage-backed attachments use the shared storage API and size limits.

## Implementation slices

1. Message builders, address validation, text/HTML templates, headers, inline and ordinary attachments, memory/log test drivers.
2. SMTP transport and API transports matching Rust's Mailgun, Postmark, Resend and SES coverage, using existing dependencies where possible; new provider accounts remain a separate operational decision.
3. Queue integration and after-commit sending using the existing jobs/outbox path.
4. Provider result mapping, lifecycle callbacks, fake transport assertions and delivery diagnostics.

## Failure behavior

Separate message construction failures, permanent recipient/provider rejections, transient transport errors and ambiguous accepted-but-response-lost outcomes. Reuse provider idempotency where available; retries can otherwise produce duplicate mail. Never log full credentials, private attachments or reset-token links.

Bound attachment memory and total message size. Template failures occur before transport submission. A successful provider acceptance is not a claim of inbox delivery.

## Acceptance

Test template escaping, invalid addresses, MIME/header injection, attachment lifetime, storage failure, provider status/error mapping, retry classification, queued serialization, after-commit suppression on rollback and fake assertions. Run provider contract fixtures separately from real-account smoke tests; missing credentials remain a verification gap. Apply the [common gate](README.md#common-completion-gate).


## Implementation status

Milestone 16 passed native verification and consumer review. [The email guide](../docs/guides/email.md) owns concrete API examples and transport/ownership limits. `email.Template[T]`, `Message`, `Mailer`, storage attachment references and five provider adapters implement this blueprint. `email.JobHandler[P]` composes existing typed jobs and the shared transactional outbox instead of introducing another queue. `jobs.Permanent` makes construction/permanent/ambiguous failures terminal while transient rejection uses the existing retry policy. Provider acceptance is distinct from inbox delivery.

No dependency was added: MIME/SMTP/templates use the standard library, SES reuses the existing AWS SDK signer and credential interface. Rust's transport coverage is preserved with Postmark header arrays and SES raw MIME attachment support. External account smoke sends remain an explicit verification gap until approved credentials, sender and recipient are available. Milestones 17–24 and the final whole-framework audit remain required.

The [master evidence](00-master-architecture-and-parity.md#milestone-16-verification-and-consumer-review) records acceptance, review corrections and the real-provider smoke gap.
