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
returning escaped HTML text. Calls share one immutable empty-allowlist policy with
the default bounds, built on first use.
Results are for HTML body content, not JavaScript, CSS, URLs or attribute contexts.
The underlying sanitizer uses an HTML tokenizer and does not promise to repair
malformed nesting into a balanced document.

## Collections

`collection` transforms ordinary typed slices. `Map`, `Filter`, `FlatMap`,
`Find`, `Any`, `All`, `Count`, `Reduce`, `Partition`, `KeyBy`, `GroupBy`,
`UniqueBy`, `SortBy`, `SortByDesc`, `MinBy`, `MaxBy`, `CountBy`, `SumBy` and
`AverageBy` execute callbacks synchronously. Result containers are independent;
pointer/map/slice elements keep normal Go reference semantics. Callbacks obey the
ordinary Go nonnil/function contract.

`KeyBy` keeps the last value per key, `UniqueBy` keeps the first, and `GroupBy`
preserves input order inside groups. Map iteration has normal unspecified order.
Slice transformations preserve nil input. `All` is true for empty input.

`Chunk` returns chunks of at most `size` items that share one new backing array,
independent of the input; each chunk's capacity equals its length, so an append
never overwrites the next chunk. A size below one panics, like `slices.Chunk`.
`Unique` keeps the first occurrence of each comparable value. `SortBy` and
`SortByDesc` return a new, stably sorted slice and run the key callback once per
item; `MinBy`/`MaxBy` return the first item with the smallest/largest key and
`false` for empty input. `CountBy` counts items per key.

`Sum`/`SumBy` add built-in numbers with ordinary Go arithmetic, including integer
wraparound. `Average`/`AverageBy` return a float64 mean and `false` for empty
input; the running total is float64. Use `decimal.Sum` and `Div` for exact money
or decimal totals.

Use the Go standard library for operations it already owns: `slices.Chunk` when
aliasing subslices are acceptable, in-place `slices.SortFunc`, `slices.Reverse`,
`slices.Clone`, `slices.Min`/`Max`, slice expressions for take/skip, and
`iter`/`maps` for iteration. Standard operations keep their own aliasing and
empty-input contracts; the framework adds no mutable collection wrapper.

## Strings

The `str` package provides rune-safe helpers; none modifies its input.

- `Slug(text)` lowercases, folds Latin diacritics to ASCII (`"Crème Brûlée"` →
  `"creme-brulee"`), keeps letters and digits from other scripts, turns
  whitespace/`-`/`_` runs into one `-` and removes other punctuation
  (`"Don't stop!"` → `"dont-stop"`). The result can be empty; validate it where a
  slug is required.
- `Limit(text, limit, suffix)` keeps at most `limit` runes, trims trailing
  whitespace and appends `suffix` only when text was shortened.
  `Truncate(text, width, suffix)` keeps the whole result, suffix included, within
  `width` runes. Neither splits a UTF-8 sequence.
- `Mask(text, mask, start, length)` masks runes from `start`; a negative start
  counts from the end and a negative length masks through the end.
  `Mask("taylor@example.com", '*', -15, 3)` is `"tay***@example.com"`.
- `Plural`, `Singular` and `Pluralize(text, count)` inflect the last word of
  English text with regular suffix rules, a fixed irregular table and uncountable
  nouns, preserving capitalization (`"Person"` → `"People"`). They are for labels
  and identifiers, not a complete English grammar.
- `HumanFileSize(size, precision)` uses 1024-based IEC units (`B`, `KiB` … `EiB`)
  and at most `precision` (0–6) fractional digits, rounded half up without
  trailing zeros: `HumanFileSize(1536, 1)` is `"1.5 KiB"`.

## Exact decimals and money

`decimal.Decimal` arithmetic stays exact; see [database codecs](database-codecs.md#exact-decimals)
for parsing, JSON and SQL. `Cmp` compares canonical digits without allocating.
`Sign`, `Neg`, `Abs`, `decimal.Min`, `decimal.Max`, `decimal.Sum` and
`decimal.Scaled(coefficient, scale)` (for example `Scaled(1234, 2)` is 12.34) never
round.

Rounding always names a `decimal.RoundingMode`: `HalfUp` (ties away from zero),
`HalfEven` (ties to even), `Down` (toward zero), `Up` (away from zero), `Floor` and
`Ceiling`. The zero mode is invalid. `Round(scale, mode)` returns at most `scale`
fractional digits and leaves values that already fit unchanged.
`Div(divisor, scale, mode)` rounds the exact quotient once; division by zero, a
negative scale, a scale above `MaxDigits` and results beyond `MaxDigits` fail.
`Fixed(scale)` presents exactly `scale` fractional digits (`"2.50"`) and fails
instead of rounding when the value has more digits.

```go
rate, _ := decimal.Parse("0.0825")
tax, err := subtotal.Mul(rate)
if err == nil {
	tax, err = tax.Round(2, decimal.HalfEven)
}
share, err := total.Div(decimal.FromInt64(3), 2, decimal.HalfUp)
```

`decimal/money` pairs an amount with a `money.Currency` ISO 4217 code.
`ParseCurrency` accepts three uppercase letters naming a monetary currency known
to the CLDR data in `golang.org/x/text`, plus the newer codes MRU, SLE, UYW, VED,
VES, XCG and ZWG. Metals, drawing rights, test and "no currency" codes (for example
XAU, XDR, XTS, XXX) are rejected. `MinorUnits` follows ISO 4217: JPY/KRW and the
other zero-decimal currencies use 0, BHD/IQD/JOD/KWD/LYD/OMR/TND use 3, CLF/UYW use
4 and other currencies use 2.

`money.New(amount, currency)` rejects amounts with more fractional digits than the
currency allows; `NewRounded` rounds with an explicit mode and `FromMinor(units,
currency)` converts integer minor units. `Add`, `Sub` and `Cmp` require the same
currency. `Multiply(factor, mode)` rounds a tax, discount or quantity product back
to minor units. `Allocate(ratios...)` and `Split(parts)` never create or lose a
minor unit: each part receives the floor of its share and leftover units go to the
largest remainders, earlier parts first on ties. Splitting 100.00 USD three ways
yields 33.34, 33.33 and 33.33. At most `money.MaxAllocationParts` parts are
produced. `MinorUnits()` fails instead of wrapping beyond int64.

Money JSON is an object with a decimal string at the currency's scale, such as
`{"amount":"12.30","currency":"USD"}`. Decoding requires exactly those two string
fields and rejects unknown/duplicate fields, numbers and excess fractional digits.
The zero `Money` has no currency and does not serialize. Persist amount and
currency as separate `decimal.Decimal` and text columns; `Money` has no SQL codec.

## Locale number formatting

`numberformat.New(locale)` derives one locale's digits, decimal and grouping
separators, grouping sizes (including Indian `12,34,567` grouping), minus sign and
percent pattern from the CLDR data in `golang.org/x/text`, using exact integer
probes. Retain the immutable `*Formatter`; it is safe for concurrent use. Values
are formatted from exact decimal text and never pass through float64.

- `Number(value)` shows every canonical digit: `1234567.5` is `1,234,567.5` in
  `en`, `1.234.567,5` in `de` and `١٬٢٣٤٬٥٦٧٫٥` in `ar` (use `ar-u-nu-latn` for
  Latin digits).
- `Fixed(value, scale, mode)` rounds and shows exactly `scale` digits.
- `Percent(ratio, scale, mode)` multiplies by 100 exactly: `0.125` is `12.5%` in
  `en`, `12,5 %` in `de` and `%12,5` in `tr`.
- `Money(amount, display)` uses the currency's minor units and a `Symbol`,
  `NarrowSymbol` or ISO `Code` display: `$1,234.50` in `en`, `1.234,50 €` in `de`,
  `CHF 12.00` for a code. The minus sign precedes the whole currency pattern.

Symbol placement follows CLDR standard patterns for a documented table: symbol
after the number for ar, be, bg, ca, cs, da, de, el, es, et, fi, fr, he, hr, hu,
is, it, kk, lt, lv, nb, nn, no, pl, pt-PT, ro, ru, sk, sl, sr, sv, uk and vi; symbol
and space before it for nl, pt, de-AT, de-CH, de-LI and it-CH; and symbol directly
before it elsewhere. A symbol that ends in a letter, such as `CHF`, is separated by
a no-break space. `WithCurrencyPlacement` returns a copy with an explicit
placement. The data is the CLDR version bundled with `golang.org/x/text`; for
example it groups four-digit numbers in every locale.

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
