# Tagged payload unions

T02 passed the native gate, typed consumer/client/compiler/editor checks, fuzzing
and [cost measurements](../evidence/typed-api-t02.json). The
[master](../../blueprint/00-master-architecture-and-parity.md#typed-api-delivery)
owns milestone status.

Declare ordinary payload DTOs and list their Go types once:

```go
//foundry:dto
type CardDTO struct {
    Token string `json:"token"`
}

//foundry:dto
type BankDTO struct {
    Reference string `json:"reference"`
}

//foundry:union name=PaymentMethod discriminator=kind
type PaymentMethodVariants struct {
    Card CardDTO `union:"card"`
    Bank BankDTO `union:"bank_transfer"`
}
```

`PaymentMethodVariants` is the generator declaration. Endpoints and application
code use the generated `PaymentMethod`. Payload fields and tags come from the DTOs;
there is no separately maintained JSON or TypeScript schema. Generation rejects
duplicate tags, payload fields named `kind`, invalid payloads and symbol collisions.
Payloads may be concrete instances of generic DTOs. Ordinary scalar enums remain
separate. The first union representation is an internally tagged JSON object.

```go
method, err := PaymentMethodFromCard(CardDTO{Token: "provider-token"})
if err != nil { return err }

card, selected := method.Card() // CardDTO, bool
if selected { useToken(card.Token) }

label, err := MatchPaymentMethod(method,
    func(card CardDTO) (string, error) { return card.Token, nil },
    func(bank BankDTO) (string, error) { return bank.Reference, nil },
)
```

Constructors validate and capture an immutable JSON snapshot, following the existing
`value.JSON` ownership model. Accessors return fresh typed payloads; retain the
result if using several fields. This costs encoding at construction and decoding
at access, while response encoding reuses the captured wire value. There is no
mutable map or type assertion in consumer code, no schema reflection per request,
and no constructor-payload alias that can alter later responses. Custom field
codecs retain their existing deterministic, bounded, concurrency-safe contract.

Every callback is a required argument to `MatchPaymentMethod`, including branches
not selected at runtime. Missing arguments or wrong payload types fail compilation;
nil callbacks return an error. Adding a variant changes this signature. Ordinary
Go switches are not compiler-exhaustive. The zero/unset union is invalid; use
`value.Optional[PaymentMethod]` and `value.Nullable[PaymentMethod]` for absence/null.

Use `PaymentMethodJSON()` with existing typed HTTP bodies/responses and
`EnvelopeJSON(PaymentMethodJSON())`. Generated DTOs can contain unions, arrays of
unions, recursive object references and presence wrappers. The
[public consumer](../../tests/fixtures/consumer/unions/payment.go) demonstrates the
complete API and typed handlers.

The shared graph rejects missing/unknown/non-string or duplicate tags, mixed
variant fields and malformed payloads. Nested errors name wire fields such as
`/body/method/kind`. Parser byte/depth/node limits and graph work limits still apply.
Generated native codecs also enforce the shared maximum bounds for direct
`encoding/json` use. Invalid response values fail before success headers are sent.

OpenAPI uses `oneOf`, required literal discriminators and explicit mappings to
derived tagged components. TypeScript emits a closed discriminated union, preserves
wide numbers and exact optional properties, and validates both requests and responses.
Tagged unions were introduced in manifest format **2**. Current format **3** also
records forms and request preparation; older readers reject newer formats and
format-2 readers require regeneration of older manifests. Existing wire routes need
not change merely because their generated manifest is updated.

A new variant is a compatibility event for exhaustive clients. Release consumers
that understand the new tag before producing it for those clients, or version the
operation. Older generated clients reject unknown tags rather than dropping data
or interpreting an arbitrary shape. The acceptance harness generates a prior
contract without one variant to exercise that rejection explicitly.

Native Go/TypeScript HTTP round trips, compiler failures, real gopls, deterministic
generation, schema ownership, runtime bounds and fuzzing passed. The complete
`make verify` gate passed after the batched fixes and cache cleanup.

| Native fixture operation | Median µs | Bytes/op | Allocations/op |
| --- | ---: | ---: | ---: |
| Construct owned card variant | 9.33 | 6,054 | 134 |
| Access fresh card payload | 1.99 | 1,389 | 19 |
| Encode through JSON descriptor | 4.13 | 3,524 | 89 |
| Decode through JSON descriptor | 12.04 | 5,815 | 130 |

The strict dispatch benchmark uses the same parser, shape checks and owned wire
snapshot for the handwritten baseline. Both paths reject unknown tags and fields.

| Variants | Contract median µs | Handwritten median µs | Contract / handwritten allocations | Wire bytes |
| --- | ---: | ---: | ---: | ---: |
| 2 | 0.83 | 0.65 | 27 / 25 | 27 |
| 32 | 0.86 | 0.66 | 28 / 26 | 28 |

Two/32-variant private consumers also measured generated size, cold/unchanged/edited
generation and builds, and three fresh gopls completion/hover sessions.

| Variants | Generated bytes | Generation seconds (cold / unchanged / edited) | Build seconds (cold / unchanged / edited) |
| --- | ---: | --- | --- |
| 2 | 6,172 | 4.49 / 0.24 / 0.47 | 2.10 / 0.23 / 0.24 |
| 32 | 39,406 | 4.03 / 0.24 / 0.48 | 2.08 / 0.24 / 0.24 |

Runtime medians use five samples. Generation/build times are single sequential
local-checkout samples with isolated compiler caches and existing module downloads;
they do not establish application-wide throughput or packaged-release performance.
The evidence retains raw samples, editor timings, commands, source hashes and the
measurement harness. Temporary compiler caches were cleaned after measurement.
The remaining T-series milestones and final integrated audit subsequently passed;
see the [integrated workflow and acceptance](typed-api-workflow.md).
