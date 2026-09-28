# Typed API experience: source audit

Initial pre-implementation snapshot, reviewed 2026-09-18 against Foundry-Go
implementation and test sources. The findings below describe that snapshot, not
the later implementation. Current delivery and execution evidence belongs to the
[master](../blueprint/00-master-architecture-and-parity.md) and the
[implementation audit](guides/typed-api-audit.md).
The [new continuation](../blueprint/typed-api/README.md) addresses the confirmed
gaps without replacing delivered systems.

## Findings on the eight claims

| Claim | Finding in Foundry-Go | Evidence and remaining work |
| --- | --- | --- |
| Handler inputs, output and HTTP contract can disagree | Already substantially addressed | [Endpoint](../http/endpoint.go) binds `Handler[P,Q,B,R]` to the same typed descriptors used for decoding, response encoding and [metadata](../http/endpoint_metadata.go). [Response](../http/endpoint_payload.go) owns the success status. [Application error declarations](../http/error_declaration.go) share runtime metadata; undeclared custom errors become internal failures. [Pagination](../http/pagination/response.go) returns generic DTO envelopes. Ordinary Go `error` is not a compiler-enforced exhaustive error union, and raw handlers intentionally have no fabricated contract. Preserve these limits; no replacement HTTP/resource subsystem is justified. |
| Payload unions and reusable generic schema generation are missing | True for tagged unions; partial for generics | The [schema kind inventory](../contract/schema.go) has no union/discriminator representation. [Discovery](../internal/generate/discover_dto.go) rejects non-empty interface fields without a declared custom JSON implementation. [Declaration discovery](../internal/generate/discover.go) rejects directly annotated generic declarations; [negative tests](../internal/generate/dto_test.go) include that case. Concrete generic values/custom contracts already have [identity coverage](../internal/generate/dto_identity_test.go), and pagination is generic. Add generic DTO template generation and first-class discriminated unions, not a replacement for all existing generic support. |
| PATCH loses omitted/null/value | False for the supplied typed path | [Optional and Nullable](../value/optional.go), [presence tests](../value/optional_test.go), [HTTP rules](../tests/fixtures/consumer/validationrules/presence_http_test.go), [mutation drafts](../tests/fixtures/consumer/inputqueries/inputs_test.go) and [strict TypeScript cases](../tests/fixtures/consumer/clientcontracts/testdata/types.ts) already preserve the distinction. Consumer mapping to a draft remains explicit. Add an integrated HTTP-to-database/client regression in T07; do not introduce a second `PatchField` abstraction. |
| Query/form validation and useful decoding errors are absent | Partly false | [Query decoding](../http/query.go), [source-specific validation](../http/endpoint_validation.go) and [input error translation](../http/endpoint.go) exist. Invalid transport shapes have safe field paths and HTTP 400; rule failures use 422. [Body kinds](../http/endpoint_payload.go) include JSON and multipart, but no URL-encoded form. There is no reusable typed normalization/request-authorization stage in `Endpoint.serve`; existing [authentication](../http/authentication_binding.go) and policies remain available. Add form transport and explicit request hooks without changing the 400/422 distinction. |
| Binding only supports one primary-key parameter | Partly false | [ByKey](../http/modelbinding/key.go) has a concrete path selector; [Define/Resolve](../http/modelbinding/resolver.go) supports custom keys and scopes. The [consumer](../tests/fixtures/consumer/httpmodels/bindings.go) filters a child through its parent's ID. [Bind](../http/modelbinding/endpoint.go) delivers one model; loading a parent and returning a typed parent/child bundle still needs consumer callbacks. Add reusable alternate-key and nested composition helpers over those queries/resolvers. |
| Complete HTTP tests lack automatic database isolation | A real convenience/integration gap | [PostgreSQL test helpers](../testkit/postgres/postgres.go) create a unique retained schema, but `Open` does not scope the pool to it. [Adapter configuration](../database/postgres/config.go) has no schema/search-path setting. [Consumer auth](../tests/fixtures/consumer/bootstrap/auth.go) manually wraps each operation in `withinSchema`; [persistent feature setup](../tests/fixtures/consumer/bootstrap/persistent_test.go) assigns schema settings individually. Existing tests can isolate data, but a test-owned application pool does not automatically direct ordinary handler queries into the new schema. |
| Reusable inbound idempotency is missing | Confirmed source gap | A repository search found outgoing retry/provider keys and job/event identity facilities, but no inbound HTTP operation claim, request fingerprint and response replay store. [Outbound retry policy](../httpclient/config.go), [job identity](../jobs/execution.go) and [event outbox identity](../events/queued_outbox.go) are not that facility. Add a PostgreSQL transaction-bound operation runner with an HTTP adapter. |
| Durable dispatch after commit is missing | False | [Event enqueue](../events/outbox.go), [job enqueue](../jobs/outbox.go), [migrations](../outbox/migrations.go), the [publisher](../outbox/publisher/publisher.go) and [configured assembly](../application/outbox_providers.go) already exist. [Guide](guides/outbox.md) distinguishes transactional enqueue from process-local callbacks and at-least-once delivery. Preserve it; T07 adds composition/crash/replay proof with inbound idempotency and a duplicate-safe receiving example. |

## Assessment boundaries

The claims were checked against public source, generator implementation, generated
consumer usage and test sources. Test source is evidence of intended and covered
cases, not a claim that a fresh test run occurred today. No runtime defect was
established merely because a convenience API is absent. Historical guide sentences
that still call delivered integrations pending do not override implementation and
the master acceptance record.

The accepted direction remains Laravel-like convenience through explicit Go types,
small interfaces, ordinary errors and constructor injection. Context carries
cancellation, deadlines and request attribution; it is not a global service bag.
PostgreSQL remains the only database engine. Runtime speed, allocations, generated
code size, compile cost and editor cost need measurements; using Go alone does not
establish a performance guarantee.

## Resulting scope

T01–T06 add the missing capabilities and reduce confirmed consumer boilerplate.
T07 verifies their composition with existing contracts, PATCH, typed errors,
pagination and outbox, then audits and improves the complete changed implementation.
The master alone owns milestone status. This audit does not reopen completed
foundation or consumer-startup milestones and does not claim the new work is done.
