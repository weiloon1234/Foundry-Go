# HTTP contract and lifecycle changes

These instructions add to the [repository rules](../AGENTS.md). Start from the
closest public endpoint/middleware consumer. The [typed API workflow](../docs/guides/typed-api-workflow.md)
shows composition; [request contracts](../docs/guides/http-requests.md) own decoding
and request lifecycle details.

## One handler contract

- Keep path/query/body/response types connected through the actual endpoint and
  handler. Declared status, media type and application errors must match runtime
  output and exported manifest/OpenAPI/TypeScript metadata.
- Reuse `contract`, `validation` and generated descriptors. Do not maintain a
  separate hand-written response schema or use untyped JSON to bypass a missing API.
- Preserve typed generic envelopes, union discriminators, model-owned identifiers,
  lossless wide numbers and typed pagination through every adapter.
- Keep decoding, preparation, prohibited-input checks, validation and authorization
  in the established order. Decode failures retain useful field paths; preparation
  must not erase evidence that a prohibited field was originally supplied.
- Preserve omitted/null/value and explicit zero/false/empty values. Different media
  types keep their own cardinality and parsing rules; form text `null` is not JSON null.

## Authority, effects and transport ownership

- Guarded handlers receive concrete actors. Nested binding resolves the child through
  its declared parent scope; alternate keys must not bypass tenant/resource policy.
- Idempotency claims and replay retain current authorization, caller/operation scope,
  payload comparison and the exact stored response contract. Use the runner's actual
  transaction for atomic business writes and durable follow-up work.
- Preserve body/stream closure, request capacity, asset leases and drain ownership
  across failures and cancellation. Avoid formatting arbitrary I/O errors in logs.
  Existing typed faults own public status/messages and safe diagnostic context.
- Middleware changes must preserve documented ordering, headers, body limits,
  streaming/abort behavior, proxy trust and authentication/CSRF protections.

## Evidence to select for the completed batch

Exercise actual handlers/kernels and clients for changed wire behavior, including
failure responses and a healthy request after failure. Use real database tests for
binding/idempotency/persistence changes. Contract changes need exported metadata,
strict TypeScript compilation/runtime and meaningful Go compile-fail coverage.
Use relevant races/fuzzing for changed concurrency/parser boundaries; do not repeat
full editor/compiler suites after each minor middleware edit.
