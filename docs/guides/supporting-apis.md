# Supporting APIs

Milestone 20 passed native verification and consumer review. These helpers use
focused packages and ordinary Go values. See the
[master evidence](../../blueprint/00-master-architecture-and-parity.md#milestone-20-verification-and-consumer-review)
for checks and operational limits.

## Cryptography and tokens

Use the existing [encryption package](encryption.md) for versioned authenticated
encryption and retained-key rotation, `auth/password` for password hashing and
the HTTP URL-signing API for purpose-bound signatures. Milestone 20 reuses those
implementations and their compatibility tests.

`randomtoken.Bytes(size)` obtains cryptographic entropy. `Hex(size)` and
`Base64(size)` encode that number of bytes; `Generate(length)` returns exactly that
many alphanumeric characters using rejection sampling. Requests are bounded to
1–4,096 bytes/characters. Encoded tokens return `secret.String`, so routine
formatting/serialization is redacted; transmission requires `Reveal()`. Raw bytes
are an explicit unredacted boundary. Character count is not the same as entropy
byte count.

Stored authentication credentials reuse this generator while retaining the
existing 32-byte, unpadded URL-safe base64 token and SHA-256-of-raw-bytes digest
format. No password/signature/encryption primitive is replaced.

## HTML fragments

`sanitize.New(sanitize.DefaultConfig())` constructs an immutable, reusable policy
backed by the established [bluemonday implementation](https://github.com/microcosm-cc/bluemonday).
Call `policy.HTML(ctx, input)` to receive a sanitized fragment. Customize the
passive tag allowlist at construction; active tags, forms, embedded documents,
SVG/MathML, scripts, styles, event handlers and data attributes cannot be enabled.
An unsupported tag request is an error.

Links accept parseable HTTP, HTTPS or mailto URLs, with no relative or data URLs.
Only `href`/`title` on enabled anchors and `src`/`alt`/`title` on enabled images are
permitted; links gain nofollow/noreferrer. Image support requires explicitly
adding `img`. The default allowlist contains basic text/list/anchor tags.

Input is bounded to 1 MiB and output to 4 MiB by default; either limit can be
reduced. Cancellation or a bound failure returns no partial fragment.
`sanitize.StripTags` removes every tag and discards active-element content,
returning escaped HTML text. Repeated calls can reuse an empty-allowlist policy.
Results are for HTML body content, not JavaScript, CSS, URLs or attribute contexts.
The underlying sanitizer uses an HTML tokenizer and does not promise to repair
malformed nesting into a balanced document.

## Collections

`collection` transforms ordinary typed slices. `Map`, `Filter`, `FlatMap`,
`Find`, `Any`, `All`, `Count`, `Reduce`, `Partition`, `KeyBy`, `GroupBy` and
`UniqueBy` execute callbacks synchronously. Result containers are independent;
pointer/map/slice elements keep normal Go reference semantics. Callbacks obey the
ordinary Go nonnil/function contract.

`KeyBy` keeps the last value per key, `UniqueBy` keeps the first, and `GroupBy`
preserves input order inside groups. Map iteration has normal unspecified order.
Slice transformations preserve nil input. `All` is true for empty input.

Use the Go standard library for operations it already owns: `slices.Chunk`,
`slices.SortFunc`, `slices.Reverse`, `slices.Clone`, `slices.Min`/`Max`, slice
expressions for take/skip, and `iter`/`maps` for iteration. Standard operations keep
their own aliasing and empty-input contracts; the framework adds no mutable
collection wrapper.

## Temporal helpers

Configured applications expose `s.Time()` with the injected clock and the
[application timezone](application-timezone.md). Retain this concrete
`temporal.Service` for `Now`, `Today`, `Parse`, `Format`, `StartOfDay` and
`AddDays` without repeating the zone. `s.Calendar()` supplies matching schedules.

`temporal.Now(clock)` and `Today(clock, zone)` receive the application's injected
clock explicitly. Clock panics/Goexit are isolated. Existing immutable `DateTime`,
`Date`, `Time` and `LocalDateTime` values retain their standard `time` bridges.

`DateTime.DateIn(zone)` selects the calendar date. `FormatIn(zone)` preserves the
offset in RFC3339Nano; offsets not representable without losing precision are
rejected. `UnixMilli`/`UnixMicro` retain integer timestamps.
`ParseDateTimeIn(text, zone)` accepts an explicit RFC3339 offset, otherwise uses
the existing strict local-wall-time resolver. DST gaps fail and overlaps require
an explicit offset; there is no automatic ambiguous-time choice.

Use `Add(time.Duration)` for elapsed arithmetic and `Date.AddDays` for calendar
days. Formatting and other standard operations can use `DateTime.UTC()` and Go's
`time` package. No helper reads the machine's implicit timezone or changes a global
clock.
