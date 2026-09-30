# Form controller follow-up for the starter

Status: framework implementation and migration candidate accepted; publication
and actual starter adoption remain pending. Pin the published revision when it
exists and regenerate the SDK through the framework. The
[master](../../blueprint/00-master-architecture-and-parity.md) records acceptance,
and the [form guide](client-forms.md) owns API/lifecycle details.

The starter was rechecked after its descriptor adoption: it selects
`v0.0.0-20260930053505-b245eb57e3fc`, has manifest v6 and already declares project
name/status/budget/description presentation, plus credential/file hints. Keep that
work. The earlier stabilization migration patch is historical; this follow-up
requires no database migration, queue layout change or replacement of that patch.

## Prepared migration candidate

A migration patch was prepared in a private, untracked workspace. It is not part
of this repository; re-derive it from the steps below when upgrading the starter.
It added the optional adapter flags to the starter's existing export command,
used `createForm` in the existing project-create HTTPS test, and extended the
installed-package test sources with exact-value, field-owner and presence checks.
Its Go export test also covered optional adapter creation, freshness and removal.
It contained handwritten changes only; regenerate owned outputs after updating
the framework requirement. The real starter has not been modified. It predates
the re-audit's submit outcome change below, so it must be updated for it.

The patch dry-run matches the current starter files. The private candidate passes
SDK build, strict TypeScript, installed-archive consumers and both real HTTPS
profiles (core and documents). The patched Go export command passes its
service-free test, including optional adapter freshness/removal. These checks use
a private Go overlay and local framework replacement with Go dependency downloads
disabled; the generated SDK archive is installed offline by the existing harness.
They do not establish published-module resolution or full macOS/Linux deployment
acceptance. Those remain part of the actual starter upgrade below. The
[framework evidence](../evidence/forms-startup-20260930.json) records the checked
sources, final framework verification and review corrections.

## Adoption after framework acceptance

After the user publishes the reviewed controller revision:

1. Update the existing single Foundry-Go runtime/tool requirement and regenerate
   owned Go, manifest v6, OpenAPI and TypeScript together. Keep independent module
   resolution and all existing privacy/installed-package checks. Review generated
   schema names if new SDK declarations collide with application type imports.
2. Add a neutral project-create/edit example using `createForm(operation(...),
   draft)`. Resolve fields from that operation and read enum choices, label/help
   keys and kinds from its existing descriptors. Do not add handwritten DTO,
   enum or validation schemas. Name/budget text uses `setText` then explicit
   `parse`; description controls distinguish omitted/null/value for patch requests.
3. Submission passes the existing authenticated client explicitly. Display the
   full validation report and server JSON Pointer issues, including form-level
   errors. Preparation remains server-only; empty client issues do not imply
   acceptance. Disable duplicate submission while `pending` remains true, and
   dispose the controller when the owning screen/request exits. Handle results
   marked `changed` (the draft moved on after sending) and reconcile a `canceled`
   result whose `outcome` is `unknown` before retrying.
4. If shipping framework-specific examples, export contracts with `--react`
   and/or `--vue`. Keep generated adapters on optional package subpaths (`./react`,
   `./vue`), with optional peer dependencies and declarations. The core entry and
   existing fetch transport must remain free of UI imports. Request approval
   before installing new starter/frontend dependencies. Actual controls, styling,
   accessible labels and layout belong to the example/frontend, not Go metadata.
5. Extend installed-package consumers with draft/presence/exact-value behavior,
   actual form calls and server errors, cancellation/stale result protection and
   cleanup. Use real React/Vue runtimes for selected adapter examples, including
   unmount and separate SSR request owners. Keep both existing HTTPS profiles and
   the normal required independent macOS/Linux/deployment checks.

## B08 stays separate

The framework now logs database startup/ready, each transient retry and terminal
failure with safe classification and elapsed/backoff information. It preserves the
connection and startup budgets and queue behavior. Modules inherit the application
logger; a manually prepared pool can opt in with `WithStartupLog`.

The starter has already retained worker stderr, selected queue state and a stack
after its original eight-second condition fails. Keep that test unchanged and
continue normal gates. Retain any real recurrence's evidence and identify the
responsible owner before fixing it. Do not run repetition-only campaigns or close
B08 based on passing repetitions. No root cause or release exception is established
by this form/controller work or the new startup logs.
