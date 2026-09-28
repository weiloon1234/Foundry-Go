# Email

Milestone 16 passed native verification and consumer review. Real-account provider
smoke sends remain an explicit verification gap. The stable blueprint contract is [email](../../blueprint/16-email.md).

`email` owns validated addresses, immutable message builders, typed templates, bounded attachment resolution and MIME. A `Mailer` borrows a transport and the existing storage registry. Use the ordinary [jobs](jobs.md) API for queued data and transactional publication; there is no separate email queue.

## Typed messages and templates

See the [consumer mailing package](../../tests/fixtures/consumer/mailing/welcome.go) for the complete verified pattern:

```go
type Welcome struct {
    Recipient email.Address `json:"recipient"`
    Name string `json:"name"`
    Documents []email.Attachment `json:"documents,omitempty"`
}

template, err := email.NewTemplate[Welcome](email.TemplateSource{
    Subject: "Welcome {{.Name}}",
    Text: "Hello {{.Name}}",
    HTML: "<p>Hello {{.Name}}</p>",
    MaxBytes: 16 << 10,
})
// Handle err before using template.
body, err := template.Render(ctx, input)
// Handle err before sending.
message := body.Message(from, input.Recipient).Attach(input.Documents...)
result, err := mailer.Send(ctx, message, email.SendOptions{})
```

`NewTemplate[T]` requires a named struct. `NewDynamicTemplate` explicitly accepts `map[string]any`; it is an escape hatch, not the normal contract. Template source is trusted application code. HTML data is escaped by `html/template`; do not inject untrusted template source or convert untrusted strings to `template.HTML`. Missing keys, evaluation failures, subject injection and output limits fail before submission. Subject/text use text templates. Rendering waits for custom methods to actually exit, including methods that ignore context. `MaxBytes` bounds the combined rendered output, not each field independently.

`ParseAddress` handles one mailbox and an optional display name. Mailboxes use ASCII local/domain parts; Unicode display names are encoded for MIME. SMTPUTF8 and implicit IDN conversion are outside this API. Zero addresses, CR/LF/control characters and address lists are rejected. The bounded validator intentionally accepts a narrower mailbox subset than every RFC edge case. JSON serialization of an address deliberately reveals its value for a declared job DTO; ordinary formatting and structured logging redact it. `Address.JSONContract`
provides the shared string wire metadata for generated DTOs and contract export,
as shown by the consumer `RecipientRequest`; runtime parsing stays in email.

`NewMessage(from, subject, to...)` has immutable `Text`, `HTML`, `Cc`, `Bcc`, `ReplyTo`, `Header` and `Attach` builders. Collections returned by accessors are copies. Custom headers cannot replace delivery/MIME/authentication headers. Limits are 50 combined recipients, 32 attachments, 32 custom headers and a 2,000-byte subject. BCC is included in the SMTP/SES envelope and provider API envelope, and excluded from MIME headers. MIME supports text/HTML alternatives, related inline parts and ordinary mixed attachments, with encoded subject/filenames and bounded physical lines. Runtime messages reject JSON serialization; queue their declared source DTO instead.

## Storage attachments and limits

An `email.Attachment` contains a typed disk ID, `storage.ObjectKey`, filename, media type, optional content ID and optional immutable version or conditional ETag. It contains no OS path, URL downloader or open reader. Use a [local storage disk](storage.md) for local files. An inline reference needs a nonempty HTML body and a unique content ID; reference it with `cid:your-id` in trusted markup.

Mailer resolves each reference through the borrowed registry, bounds reads even when metadata lies, and closes readers before transport submission or on failure. Defaults allow 16 active sends, 5 MiB per attachment, 10 MiB total encoded MIME and a 30-second operation timeout. The hard configurable MIME ceiling is 32 MiB. Encoding overhead counts against the total, so an attachment near the raw-byte ceiling can still exceed the MIME budget. Active operations retain their slot until all callbacks really return. Memory retained by application inputs, adapter encoding and optional recorders is additional to the encoded-MIME budget; this is not a process RSS limit.

Queue handlers require every attachment to pin `Version` or `IfMatch`. An ETag pins current content but does not promise historical reads; replacement then fails instead of silently mailing new bytes. Store durable attachment objects before enqueueing and retain them for the queue's retry/inspection lifetime.

## Transports

Every transport has a pure constructor. `email.New(driver, disks, config, observer)` borrows it. API configuration uses `email.DefaultHTTPConfig()` and `secret.String` credentials. Endpoint overrides require HTTPS, except explicit loopback HTTP fixtures. API requests do not follow redirects, retry, or expose replayable request bodies. A custom borrowed RoundTripper must preserve that policy. Close a provider driver after its mailer drains; `Close` releases only its owned idle HTTP connections.

| Adapter | Configuration and behavior |
| --- | --- |
| `email/smtp` | Explicit host:port and `STARTTLS`, implicit `TLS`, or loopback-only `PlainLoopback`. TLS verifies certificates, requires TLS 1.2+, and uses an optional cloned TLS config. PLAIN authentication is supported over TLS. One owned connection per message; all RCPT commands must succeed before DATA. A final DATA acceptance survives later connection-close failure. |
| `email/resend` | Token, default `https://api.resend.com`; JSON recipient arrays, headers and base64 attachments/inline content IDs. Forwards `SendOptions.IdempotencyKey`. |
| `email/postmark` | Server token, optional message stream, default `https://api.postmarkapp.com`; header **arrays**, base64 attachments and `cid:` inline IDs. Checks both HTTP status and `ErrorCode`. |
| `email/mailgun` | API key and sending domain, default US `https://api.mailgun.net`; use `https://api.eu.mailgun.net` for EU. Multipart body, `h:` headers, ordinary/inline files. Inline multipart filename uses ContentID, matching Mailgun's `cid:filename` convention. Header option bytes are capped at 16 KiB. |
| `email/ses` | Explicit AWS region and existing `aws.CredentialsProvider`; existing AWS SDK SigV4 signs `SendRawEmail`, including session credentials. Sends shared MIME/attachments and explicit envelope destinations. Optional configuration set. SES has its own 10 MiB raw-message limit. |
| `email/memory` | Explicit bounded message capacity; opt-in private in-memory snapshots for tests/development. `Messages`, `Reset` and `Close`; no delivery. |
| `email/log` | Injected slog logger; records recipient count, MIME byte count and attachment count only. Simulated acceptance; no delivery. |

Provider contracts are based on [Postmark email API](https://postmarkapp.com/developer/api/email-api), [Postmark errors](https://postmarkapp.com/developer/api/overview), [Resend send API](https://resend.com/docs/api-reference/emails/send-email), [Resend inline attachments](https://resend.com/docs/dashboard/emails/embed-inline-images), [Mailgun message API](https://documentation.mailgun.com/docs/mailgun/api-reference/send/mailgun/messages/post-v3--domain-name--messages), and [SES SendRawEmail](https://docs.aws.amazon.com/ses/latest/APIReference/API_SendRawEmail.html). Provider-side verified sender, sandbox, account, quota and attachment-type policies still apply.

## Outcomes and retries

A successful `Result.Accepted` means the transport/provider accepted the submission. It does not prove inbox delivery. Receipt IDs are opaque and redacted by ordinary formatting. The API never logs provider error bodies, authentication data, recipients, message bodies, reset links or attachment content.

| Failure kind | Meaning | JobHandler action |
| --- | --- | --- |
| `Construction` | Invalid input/template, missing or stale pinned attachment, size limit or preparation failure | Terminal failed job |
| `Permanent` | Known recipient/provider/configuration rejection | Terminal failed job |
| `Transient` | Known non-acceptance that may recover, such as explicit throttling or temporary SMTP rejection | Existing job policy retries |
| `Ambiguous` | Lost response, malformed success, unknown transport failure or uncertain server error | Terminal failed job; reconcile before explicit retry |

API network errors and generic 5xx responses are conservative `Ambiguous` outcomes. Documented rejection codes refine that: Postmark maintenance, SES throttling/service-unavailable, and Resend concurrent-idempotent-request responses are transient. Resend key/payload conflicts are permanent. Underlying error text is not returned. Custom error inspection and callbacks are isolated for panic and Goexit. Error traversal is bounded to 256 nodes and 64 unwrap levels. Incomplete driver error graphs produce `Ambiguous`; incomplete attachment error graphs fail preparation with `Construction`. Neither is automatically retried by `JobHandler`. Custom error methods must return; these bounds limit traversal, not work inside a method.

`jobs.Permanent(err)` marks terminal failure without pretending success and is preserved through ordinary wrapping/joining. It wins over timeout/stopping when the handler knows repetition is unsafe; explicit queue cancellation still retains queue semantics. Worker ownership and heartbeats continue through custom error classification.

After provider acceptance, `JobHandler` calls `jobs.PreventRetry(ctx)`. If a job
timeout or later job middleware then fails, the job records terminal failure
while the email remains accepted; it is not sent again automatically. The marker
is scoped to the live attempt and cannot survive a worker crash before finalization.

[Resend idempotency](https://resend.com/docs/dashboard/emails/idempotency-keys) retains a key for 24 hours. `JobHandler` uses `email/` plus the stable execution ID for every attempt. Ambiguous outcomes remain terminal even with a key because arbitrary redelivery can exceed provider retention. Builders must render identical content on retry: capture template inputs, recipient and pinned references in the DTO, and version the job when changing its meaning. Do not reuse one execution for several distinct messages.

Worker/process loss after acceptance but before recording success can still redeliver an email. Other adapters provide no general idempotency guarantee. There is no exactly-once claim; use provider reconciliation and domain idempotency when duplicate delivery is unacceptable. Retrying an uncertain send is an explicit operational decision.

## Jobs and transactional enqueue

Define one `jobs.Definition[Welcome]` and register `WelcomeEmail.Declare(email.JobHandler(mailer, build))`. The builder has the concrete signature `func(context.Context, Welcome) (email.Message, error)`. The existing job middleware, attribution, capture, dispatch, scheduling and worker kernel apply. `JobHandler` requires a live job context and refuses unpinned attachments.

Call `WelcomeEmail.Capture` before a retriable business transaction when its execution ID must remain stable across retries. Call the captured pending value's `Enqueue(ctx, tx, producer)` inside the transaction. The shared jobs outbox publication route exposes only committed rows; rollback suppresses delivery. `EnqueueWelcome` in the consumer fixture shows the direct one-transaction convenience path. Never call `Send` inside a business transaction expecting it to roll back.

## Lifecycle, observation and tests

`email.Module` integrates an injectable mailer with the normal foundation lifecycle. Declare dependencies on providers owning borrowed disks/transports. `Close(ctx)` cancels admission and active I/O; its context limits the caller's wait. `Done` closes only after every actual send, resolver, observer and custom error classifier exits. Close borrowed resources afterward. Self-close from an active send context returns `fault.Cycle`; preserving the supplied context is part of the callback contract.

Observers receive safe `Prepared` and `Finished` notices. A Prepared failure vetoes submission. A Finished failure sets `Result.ObserverFailed` and diagnostic counts while preserving known acceptance, avoiding accidental duplicate retries. `Snapshot` exposes counts/active/closing state without private content. Protect application diagnostic endpoints with existing authorization.

`testkit/email.AssertCount` and `AssertRecipientCount` work with the memory driver and redact assertion failure text. Test sources include MIME/escaping/injection/bounds, reader closure, callback ownership, native local SMTP/TLS/STARTTLS, provider HTTP contract fixtures, response loss, typed queue retries, real PostgreSQL rollback/commit visibility, and an independent consumer module. Compiler-rejection and real-gopls probes cover typed boundaries. These passed the native acceptance gate; see the [master evidence](../../blueprint/00-master-architecture-and-parity.md#milestone-16-verification-and-consumer-review).

Real-account sends have not been run. Provider credentials, verified sender identities and an explicitly approved recipient are required for those smokes; local protocol fixtures do not prove external deliverability. No new provider account, dependency or email to an external recipient was created by this milestone implementation.

## Explicit durable rendered snapshots

Milestone 17 adds `CaptureMessage(message)` and `Snapshot.Message()` for
notification orchestration. This explicit snapshot freezes validated addresses,
headers, bodies and pinned attachment references within `value.JSONMaxBytes`.
`Message.MarshalJSON` still rejects implicit runtime serialization. Snapshot
formatting stays redacted; its explicit JSON contains private email content and
must be protected. See [notifications](notifications.md) for delivery-time
eligibility and retry behavior. These additions passed milestone 17 verification.
