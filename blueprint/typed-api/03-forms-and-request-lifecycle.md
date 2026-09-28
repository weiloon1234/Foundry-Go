# T03 — URL-encoded forms and typed request lifecycle

Prerequisites: T01–T02. Status belongs to the
[master](../00-master-architecture-and-parity.md#typed-api-delivery).

## Existing behavior and additions

JSON, multipart, typed query parsing and source-specific validation already exist
in [HTTP](../../http/endpoint.go) and its [validation adapter](../../http/endpoint_validation.go).
Add `application/x-www-form-urlencoded` as a typed body and reusable opt-in request
preparation/authorization callbacks. Preserve current endpoints when these hooks
are absent. Reuse the validator, typed guards/policies, callback containment and
owned-resource cleanup; a new Form Request inheritance hierarchy is unnecessary.

## Form transport

Add a typed form descriptor/body adapter by reusing URL scalar codecs, query
cardinality/composition and generated field source identities. Form fields remain
`Input.Body`; URL query remains `Input.Query`. Their limits, errors, metadata and
names stay independent even when a field has the same spelling in both sources.
Never use merged query/body values from a convenience parser as the typed input.

Specify strict percent/UTF-8 decoding, plus-as-space behavior, duplicate scalar
rejection, repeated collection values, unknown-name redaction and body byte/pair/issue bounds (flat wire grammar has nesting depth zero). Unsupported media/charset, compression and oversize requests
follow existing 415/413 policies. Nested Go fields reuse explicit query composition
selectors; do not invent implicit PHP-style nesting or an unbounded key parser.

Omitted fields retain Optional absence. `name` and `name=` supply empty text;
`name=null` is literal text, not JSON null. Use JSON where three-state null is needed,
or an explicit custom typed scalar contract; no global text-to-null coercion.
Multipart uploads continue through their existing owned file path.

The actual form descriptor supplies manifest/OpenAPI media type, field metadata,
TypeScript request types and client encoding. The generated SDK selects the exact
declared media type and preserves cardinality; it does not stringify nested objects
implicitly or change unrelated JSON endpoints.

## Ordered typed stages

1. Existing transport security, signatures, authentication, CSRF and bounds run in
   their established order. No new hook bypasses them.
2. Decode path/query/body using their declared codecs. Structural issues return
   400 with safe `/path`, `/query`, `/body` pointers before typed callbacks.
3. Optional preparation receives the concrete decoded input and returns concrete
   query/body values plus an ordinary error, for example `(Q, B, error)`. The adapter
   retains the original P; the callback cannot replace path identity through its
   return value. Preserve Optional/Nullable state unless explicitly transformed.
   Callbacks must not mutate borrowed input references, descriptors or configuration.
4. Execute existing validation against the prepared input. Invalid decoded values
   return 422. Defaults/transforms must not silently satisfy a prohibited field.
5. Optional request authorization consumes the validated input and, for an
   authenticated endpoint, the concrete guard subject. Reuse existing policies
   and safe HTTP error classification. A denial never invokes domain work.
6. Bound resource lookup and resource authorization follow their owning typed
   adapter (T04); the handler then receives the prepared, validated request.
7. Existing response encoding/status/application-error contract completes the call.

Preparation happens after structural decoding. It cannot repair a string where an
integer was required or fill an absent codec-required field. A field intended to
receive a preparation default must declare Optional input. Server/domain defaults
and request defaults must remain documented at their actual owner.

Preparation is opt-in and not an arbitrary raw-body hook. Hook errors use declared
safe failures; unexpected errors/panics/Goexit remain internal. Cancellation retains
ownership until callbacks finish. Callbacks must not perform business writes; doing
so would bypass validation/idempotency boundaries. Model-based policies use T04's
post-binding stage when they require a loaded resource.

## Acceptance

- One consumer uses the same typed rules with JSON, query, multipart and forms.
  Invalid syntax, wrong type, missing required fields and rule failures prove the
  correct status, path, safe message and no handler invocation.
- Preparation order is observable: whitespace normalization, typed defaults,
  optional/null preservation and normalized-value validation. Existing unconfigured
  endpoints keep their old behavior. Query/body names cannot satisfy each other.
- Required/optional typed actors and multiple guards compose without casts;
  request denial precedes binding, and resource denial follows binding in T04.
- Form limits, duplicate/unknown keys, malicious nesting, invalid UTF-8 and request
  cancellation have bounded fuzz/race coverage and no leaked multipart resources.
- Real generated TS form submissions match manifest/OpenAPI and server behavior;
  compile-fail/gopls tests cover hook input and actor types.
- Record overhead for an endpoint with no hooks and one with preparation/authorization.

Finish all implementation/docs/tests, then execute the common milestone gate.

## Concrete T03 API

`//foundry:form` generates `Query[T]` URL bindings and typed validation fields.
`FormBody` assigns the bindings to the body source with `EndpointLimits.Form`.
`WithPreparation` accepts `func(context.Context, Input[P,Q,B]) (Q,B,error)`;
`WithAuthorization` accepts the same typed input and returns an error. Required
and optional authentication adapters add the concrete actor or `value.Optional`
actor to authorization. Configure before signing/binding. No extra form wrapper
or inherited request class is introduced.

The shared validator preflights built-in prohibitions against original input,
then checks all rules on prepared input. Conditional predicates use the input of
their respective stage. This prevents erasure of originally prohibited values;
it does not claim provenance mapping for arbitrary collection reordering.
Manifest format 3 records forms and server preparation; SDK structural checks
remain active and business validation defers to the server for prepared requests.
See the [consumer guide](../../docs/guides/forms-request-lifecycle.md).
