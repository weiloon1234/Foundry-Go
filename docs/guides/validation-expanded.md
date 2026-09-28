# Expanded rules and concurrent validation

Foundry keeps the request and field types through ordinary Go functions. This
expansion compares common needs with the [Laravel rule catalog](https://laravel.com/docs/12.x/validation#available-validation-rules),
while reusing existing Foundry presence wrappers, model queries, error handling,
metadata and generated fields. The [validation guide](validation.md) owns the
base execution contract.

## Rule coverage

| Need | Foundry API |
| --- | --- |
| Consent | `Accepted[bool]`, `Declined[bool]`; named booleans work too |
| Text case and characters | `ASCII`, `Alpha`, `AlphaNumeric`, `AlphaDash`, `Lowercase`, `Uppercase` |
| Length and numeric codes | `MinLength`, `MaxLength`, `Length`, `LengthBetween`, `Digits`, `MinDigits`, `MaxDigits`, `DigitsBetween` |
| Text membership | `Contains`, `DoesntContain`, `StartsWith`, `EndsWith`, `DoesntStartWith`, `DoesntEndWith`, `Matches`, `NotMatches` |
| Formats | Existing email, URL, IP, UUID, JSON and temporal rules; new `HexColor`, `MACAddress`, `ULID` |
| Numbers | `Min`, `Max`, `Between`, integer `MultipleOf`, exact `DecimalMin`, `DecimalMax`, `DecimalBetween` |
| Related values | `Compare` with `GreaterThan`, `GreaterOrEqual`, `LessThan`, `LessOrEqual`; `Confirmed` with two typed fields; existing `Same`, `Different` and temporal field comparisons |
| Collections | `Items`, `ItemsBetween`, `MinItems`, `MaxItems`, `ContainsItems`, `ExcludesItems`, `Distinct`, `DistinctBy`, `Each` |
| Password strength | `Password[string]` or `PasswordValue[password.Plaintext]`, using `PasswordOptions` |
| Presence and conditions | `Required`, `RequiredNullable`, `Present`, `Absent`, `Prohibited`, `Optional`, `Nullable`, `NonEmpty`; compose `When`/`Unless` instead of string field names |
| Model observations | `databasevalidation.Exists`, `Unique`, and new batched `ExistsAll` |
| Remote/application checks | Context-aware `Custom[T]`, `Lookup[T]`, `BatchLookup[T]`, and bounded `Parallel` |
| Files | Existing `FilePresent`, size, content type and extension rules; bounded image inspection belongs to [imaging](imaging.md) |

Go DTO types already enforce boolean/integer/string/list shape. `Optional` retains
wire presence; zero and false are values. `When(condition, field.Rules(Required...))`
expresses required-if; `Unless` expresses required-unless. Conditions themselves
use typed fields and rules. Validation never removes fields or normalizes input;
use the existing request preparation hook for explicit normalization.

Text lengths count Unicode code points. Digits are ASCII text so leading zeros
survive. Case checks accept uncased characters and do not modify input. Empty
text passes character-class rules; add presence or length constraints. Integer
multiples use exact remainder, including values beyond JavaScript's safe integer
range. Floating-point divisors deliberately do not compile. Password defaults
are 12–128 code points with optional letter, mixed-case, number and symbol checks;
byte bounds still apply. No password or input is copied into metadata or messages.
Password strength does not verify an existing credential or contact a breach API.

## Concurrent I/O

```go
fields := RegistrationValidationFields()
rules := validation.Parallel(
    fields.Email.Rules(validation.Bail(
        validation.Email[string](),
        validation.MaxLength[string](254),
        databasevalidation.Unique(db, models.QueryUsers(), models.UserFields().Email),
    )),
    fields.Tags.Rules(validation.Bail(
        validation.ItemsBetween[[]string](1, 8),
        validation.Distinct[[]string](),
    )),
)
```

Attach the result with `WithBodyValidation(rules)`. Go request handlers already
run independently; no async/await keyword or future is needed. Rules receiving a
context can call database or HTTP clients directly. `Parallel` runs independent
branches concurrently, defaulting to four per `Check`; `ParallelLimit` accepts
1–64. The independent [consumer](../../tests/fixtures/consumer/validationrules/expanded.go)
and [HTTP example](../../tests/fixtures/consumer/httpvalidation/async.go) are executable.

`All`, `Bail` and each field's rule chain preserve their existing sequential
semantics. Put cheap syntax checks before I/O in `Bail`. Use a concurrency-safe
pool/client for parallel branches. A shared transaction or other serial resource
belongs in a sequential group. Callbacks and selectors must not mutate input or
unsafely share state; immutable rules can serve concurrent requests.

All branches share one check budget. Bounded waves merge diagnostics in declaration
order regardless of completion order; the public issue cap still applies. Each
wave can retain at most concurrency times the remaining issue cap, also bounded
by the shared check budget. Nested parallel groups execute sequentially within
their admitted branch, keeping concurrency bounded across the tree. Conditions
share the same budget. An infrastructure failure from any admitted branch takes
precedence over ordinary rejection, even after enough field issues were collected.

Cancellation stops further waves and flows into callbacks. Already admitted
callbacks retain their capacity until actual exit, including after panic/Goexit;
an uncooperative callback can delay return. Give outbound clients suitable
timeouts and honor the provided context. Validation does not abandon goroutines.

## Named database connections

Resolve each pool during construction with
`services.Databases.Connection(name)`, where `name` is a
`database.ConnectionName`, and handle the selection error. Pass that pool as the
first argument to `databasevalidation.Exists`, `Unique` or `ExistsAll`. Each rule
retains its selected executor; `Parallel` can check different connections in the
same validation tree without changing either connection's default.

`Custom[T]` callbacks and `Lookup[T]` implementations can retain several concrete
pools or typed repositories. Pass the rule's context to every query. A failed
selected connection remains an execution error, never a fallback to the default
connection or evidence that a value is available. Checks using one transaction
should stay sequential. Checks across connections do not create a distributed
transaction or a shared database snapshot.

The [independent PostgreSQL consumer](../../tests/fixtures/consumer/model_validation_named_postgres_test.go)
exercises built-in, batched and custom rules against distinct named pools with
different retained schemas. It checks concurrent query ownership, cancellation,
pool reuse and isolation after a selected connection closes.

## Batched model lists

```go
fields := models.UserFields()
activeUsers := databasevalidation.ExistsAll(
    db, models.QueryUsers().Where(fields.Status.Eq(models.StatusActive)), fields.ID,
)
// Rule[[]model.ID[models.User]]; order IDs or string IDs do not compile.
```

`query.ValueLookup.AllExist` owns execution. It combines up to 64 parameterized
existence predicates per statement using the shared AST/compiler. This retains
SQL equality for every input, including codec representations and database
collation; it never compares a count of Go keys with SQL distinct counts. A list
of 130 valid values takes three statements, not 130. Batches stop at the first
missing value. Empty lists succeed without I/O; combine with `MinItems` when
selection is required. Duplicate inputs are allowed; use `Distinct` separately.
The validation adapter charges every item against its work limit before I/O.

Filters, named executor/transaction ownership and soft-delete scopes are retained.
Updates exclude the trusted current model ID using the existing typed `Where`
predicate shown in the base guide. Never derive that exclusion from an
untrusted field alone. Validation observations do not reserve values or replace
write authorization, unique constraints or foreign keys. Separate batches can
observe changes unless the supplied transaction provides the desired snapshot.

## Metadata and deliberate boundaries

New leaf rules are marked server-only until equivalent browser behavior is
implemented and tested. Existing range/length compositions reuse existing client
rules. Parallel metadata remains logical `All`; scheduling is not a wire contract.
Generated clients validate supported children and report `complete: false` with
skipped rule IDs for server-only checks. The server always applies the full tree.

Automatic DNS/reachability checks, password-breach services, file-content decoding,
and current-password authentication are not implicit effects of format rules.
Use the relevant injected service/custom rule for those application requirements.
Rules do not implement Laravel's dynamic string coercions or field exclusion.
Request-locale message translation and replacement of static built-in messages
are delivered in the separate [message follow-up](validation-messages.md).
The expansion passed the complete gate and relevant races; see
[acceptance evidence](../evidence/validation-expanded-20260926.json).
