# HTTP and configuration recipe extraction

Milestone 3, `lane/m3-http-config`, 2026-11-07. This is an implementation handoff,
not acceptance evidence. Work is confined to the assigned worktree. No application
repository, framework runtime, checker, Makefile, changelog or unrelated guide was
changed.

## Status and limits

The five assigned skills have an inventory below. The owned guides now have
navigation-only agent entries, explicit recipe anchors and source directives for
the extracted executable examples. Existing pilot owners remain authoritative.
No new generated DTO, enum, message or configuration declaration was introduced;
new fixture functions reuse existing generated contracts and public APIs.

Changed Go files were formatted with `gofmt`. No tests, framework builds, typechecks,
verification commands or generator checks were run. The explicitly permitted
`go run ./internal/cmd/renderdocs` write attempt failed before the renderer ran:
the sandbox denied access to the existing Go build cache under
`/Users/weiloon/Library/Caches/go-build`. Following COMMON.md, rendering is left
to the lead. New source-bound fences remain empty until that command runs; old
pilot fences have not been manually normalized. Compilation and current-source
acceptance remain unverified.

## Inventory conventions and provenance

`K/<skill>:line` means
`/Users/weiloon/Projects/rust-go/Foundry-Go-Starter/.claude/skills/<skill>/SKILL.md`
at the cited line. Region paths below are relative to
`tests/fixtures/consumer/` unless prefixed with `goway/`, which is relative to
`tests/fixtures/`. A path followed by `#name` identifies a doc-example region,
not a source-code line anchor.

Read COMMON.md, PROPOSAL.md sections 1–3, the milestone 3 scope, DECISIONS.md,
the pilot's recipe ownership and normalization sections, and the compiled-example
authoring guide. The current ooo `foundry-config` and Boss Club `foundry-auth`, `foundry-config`
and `foundry-validation-i18n` common text agrees with Starter; their differences are
application catalog sets, paths, profiles and runtime choices. Public config,
locale precision and frontend plural constraints retain that provenance without
copying application policy into framework examples.

The tables group related steps as recipes. Type catalogs, constructor-family
reference lists, framework guarantees and application workflow are distinguished
from additional executable recipes, following the pilot's inventory convention.

## foundry-auth

| Skill lines and recipe | Guide owner | Fixture region |
| --- | --- | --- |
| 21–24, 41–43, 55: concrete stored-model provider | [Declare provider](../guides/authentication.md#recipe-declare-provider) | `goway/guideadapters/examples_test.go#authentication-account` (existing) |
| 25–27, 41–43, 58–61, 101–105, 115–117: register guards, policies and permissions for the actor | [Register guards](../guides/authentication.md#recipe-register-guards) | `authenticating/accounts.go#register-guards` |
| 21, 28–32: declare and construct typed scopes; configure an authenticated endpoint | [Access scopes](../guides/authentication.md#recipe-access-scopes) | `authenticating/access_scopes.go#declare-scopes`, `#configure-scopes` |
| 21–24, 28–32: declare current account permission and apply it | [Permissions](../guides/authentication.md#recipe-declare-permission) | `authenticating/permissions.go#declare-permission`, `#configure-permission` |
| 22–24: declare and evaluate a concrete resource policy | [Resource policy](../guides/authentication.md#recipe-resource-policy) | `authenticating/accounts.go#resource-policy`, `#check-resource-policy` |
| 28–35, 58–61: capture bearer credentials, bind exact guard, require scopes and permissions | [HTTP guard binding](../guides/authentication.md#recipe-bind-guard) | `authenticating/guide_routes.go#bind-token-guard` |
| 33–38, 125–128: actor-scoped lookup, staged authenticated binding and resource authorization | [Scope resource access](../guides/http-model-binding.md#recipe-scoped-resource-policy) | `authenticating/guide_routes.go#scoped-resource-policy` |
| 44–46, 55: password login and shared provider | [Password login](../guides/authentication.md#recipe-password-login) | `passwords/login.go#password-login` |
| 44–54, 118–122: typed Redis store keys, WithRedisStores, resolution, DefineLogin and WithLockout | [Credential protection](../guides/authentication.md#recipe-credential-protection) | `authenticating/credential_stores.go#credential-stores`, `#password-lockout` |
| 55–57: shared token store and declared scope ceiling | [Token store](../guides/tokens.md#recipe-token-store) | `authenticating/tokens.go#token-store` |
| 25–27, 55–57: issue only from trusted proof with typed granted scopes | [Token issuance](../guides/tokens.md#recipe-issue-token) | `authenticating/tokens.go#issue-token` |
| 56–57: shorten a guard's renewable lifetime within the store ceiling | [Token lifetimes](../guides/tokens.md#recipe-token-lifetimes) | `authenticating/guide_routes.go#token-lifetime` |
| 74–75: native JSON token response and refresh body | [Native delivery](../guides/tokens.md#recipe-token-delivery) | `authenticating/token_routes.go#native-token-delivery` |
| 76–82: browser cookie declaration, access-only response, cookie refresh and refresh-authenticated logout | [Browser delivery](../guides/tokens.md#recipe-browser-refresh) | `authenticating/guide_routes.go#refresh-cookie`; `authenticating/browser_tokens.go#browser-token-delivery`, `#browser-token-logout` |
| 62, 74–82: rotate a supplied refresh secret | [Refresh](../guides/tokens.md#recipe-refresh-token) | `authenticating/tokens.go#refresh-token` |
| 83–85: guarded single-use WebSocket ticket issuance | [Tickets](../guides/tokens.md#recipe-token-tickets) | `authenticating/guide_routes.go#token-ticket`; existing datatable adapter remains in its original region |

No owned recipe is unresolved. Credential route POST/TLS/no-store behavior,
revocation, logout-all, failure privacy and scope freshness remain in their
existing token/authentication prose. Credential quotas link to their rate-limit
owner. Actor models, provisioning and grant policy, `*` account capabilities,
canonicalization choices, filenames, migrations, seeders and execution cadence
remain application-owned. This lane does not claim new runtime or security tests.

## foundry-middleware

| Skill lines and recipe | Guide owner | Fixture region |
| --- | --- | --- |
| 16–19: define typed native middleware with a stable ID | [Declare middleware](../guides/http-middleware.md#recipe-attach-middleware) | `httpmiddleware/router.go#middleware-scope` |
| 20–25, 60–61: endpoint, nested scope and global attachment | [Attach middleware](../guides/http-middleware.md#recipe-attach-middleware) | `httpmiddleware/router.go#middleware-route`, `#middleware-scope`, `#middleware-global` |
| 26–35, 55–65: proxy ordering, locale ownership, credential limits and policy ownership | [Attachment/order](../guides/http-middleware.md#recipe-attach-middleware), [credential quotas](../guides/http-middleware.md#recipe-credential-quota) | Existing middleware source plus `httpmiddleware/router.go#middleware-credential-group`; built-in constructor reference guides stay linked |
| 29–31: configured locale selection | [Configured messages](../guides/validation-messages.md#recipe-configure-messages) | `configured/catalog.go#configured-catalog` |
| 36–45: authenticated idempotency with current resource policy, trusted caller scope, transactional writes and outbox | [Idempotent endpoint](../guides/idempotent-operations.md#recipe-idempotent-endpoint) | `idempotenthttp/application.go#authenticated-idempotency` |

No owned recipe is unresolved. The idempotency owner retains supported/unsupported
transport rules, replay checks, unknown outcomes and resource bounds. The example
uses the real consumer's authenticated model-binding adapter and transaction,
instead of an approximate handwritten endpoint. Proxy networks, CSP sources,
fail-closed choices, portal mount policy and local inspection commands stay in
the app. Built-in CORS, CSRF, proxy and header reference guides are linked rather
than reproduced.

## foundry-endpoint

| Skill lines and recipe | Guide owner | Fixture region or existing proof |
| --- | --- | --- |
| 16–25: DTO presence, PATCH omission, query values, pointers and collections | [Presence](../guides/http-presence.md#recipe-presence), [PATCH/query](../guides/http-presence.md#recipe-patch-query) | Existing `validationrules/presence.go#presence-input-rules`; `httpdto/dto.go#patch-presence`, `httpquery/query.go#query-presence` |
| 23–25: generated labels and kinds | [Field labels](../guides/validation.md#recipe-field-labels) | `clientcontracts/dto.go#client-field-label` |
| 16–18, 26–53, 106–119: typed paths/DTO descriptors, explicit response mapping, scopes, endpoint construction, preparation, validation and decoded service inputs | [Compose endpoint](../guides/http-endpoints.md#recipe-compose-endpoint) | Pilot `httpendpoints/guide.go#compose-endpoint`; unchanged |
| 57–61: owner-scoped resolver, authenticated binding and resource policy | [Scoped resource policy](../guides/http-model-binding.md#recipe-scoped-resource-policy) | `authenticating/guide_routes.go#scoped-resource-policy`; existing ByKey/parent lookup regions retained |
| 60–61: ByColumn, ThroughRelation and ThroughSelectedRelation constructor family | [Binding owner](../guides/http-model-binding.md#recipe-scoped-resource-policy) links [scoped model binding](../guides/scoped-model-binding.md) | Existing `nestedbindings/bindings.go` regions already owned by scoped-model-binding; no second inclusion |
| 62–64: concrete public row declaration, Select/Map/MapSelected and Read | [Declare rows](../guides/model-projections.md#recipe-declare-rows), [select rows](../guides/model-projections.md#recipe-select-rows) | Pilot `reports/rows.go#user-summary`, `reports/guide.go#paginate-projection`; unchanged |
| 65–69: numbered HTTP pages and native ORM page requests | [Numbered pages](../guides/http-pagination.md#recipe-numbered-page), [authenticated pages](../guides/http-pagination.md#recipe-authenticated-pages) | Existing pilot pagination regions; unchanged |
| 73–91: declared public errors, structured details, exact endpoint registration, translated message definition and generated narrowing | [Error details](../guides/http-application-errors.md#recipe-error-details), [client narrowing](../guides/http-application-errors.md#recipe-client-narrowing) | Pilot `clientcontracts/member_create.go` and `testdata/error-details.ts` regions; unchanged |

No additional owned endpoint recipe is unresolved. Pilot coverage is reused, not
claimed as new work. The annotation/transport catalogs and typed relation helper
families remain focused reference reading, as in the pilot. App route helpers,
route IDs, package layout, domain services and disclosure decisions stay local.

## foundry-config

| Skill lines and recipe | Guide owner | Fixture region |
| --- | --- | --- |
| 19–28, 78–83: typed root/groups, defaults, validation, secret values and generated loading | [Declare configuration](../guides/generated-configuration.md#recipe-declare-configuration) | `startupconfig/settings.go#deployment-settings`, `#configuration-defaults`, `#configuration-validation`; `startupconfig/load.go#load-configuration` |
| 19–24: reuse application.Settings and one configured locale set | [Configured messages](../guides/validation-messages.md#recipe-configure-messages) | `configured/catalog.go#configured-catalog` takes application.Settings and derives locales from its feature settings |
| 36–43: declare runtime key, codec and presentation; register with the extension feature | [Declare settings](../guides/settings.md#recipe-declare-settings) | `configured/runtime_settings.go#declare-runtime-setting` |
| 44–49: preserve seeded values; absence default; upsert and joined transaction writes; existing-only Set semantics | [Write settings](../guides/settings.md#recipe-write-settings) | `configured/runtime_settings.go#seed-runtime-setting`; Set/SetIn distinction retained as reference prose |
| 50–53: typed version upgrade and post-migration startup reconciliation | [Upgrade settings](../guides/settings.md#recipe-upgrade-settings) | `configured/runtime_settings.go#upgrade-runtime-setting` |
| 57–64: explicit dotenv, process environment and typed overrides with safe provenance | [Dotenv input](../guides/generated-configuration.md#recipe-load-dotenv) | `startupconfig/load.go#load-dotenv` |
| 65–66: whole named-service tables and typed selection | [Named services](../guides/named-services.md#recipe-named-services), [table inputs](../guides/named-services.md#recipe-named-tables) | `configured/settings.go#named-services`, `#select-named-cache` |
| 67–70: public Go tags and client environment metadata | [Public configuration](../guides/generated-configuration.md#recipe-public-configuration) | Existing `configured/settings.go#generated-configuration-settings` |
| 71–72: typed durations and application timezone | [Typed configuration](../guides/generated-configuration.md#recipe-declare-configuration), [timezone reference](../guides/application-timezone.md) | Duration in `startupconfig/settings.go#deployment-settings`; timezone reference remains with its existing owner |

No owned recipe is unresolved. The app's `config:check`/`config:inspect` launcher,
profiles and operator authorization remain app workflow, not framework CLI APIs.
The public-config example remains the single Go declaration owner; browser
consumption belongs to the client lane. Settings presentation does not authorize
endpoints or encrypt secrets. This lane changes no migrations or stored data.

## foundry-validation-i18n

| Skill lines and recipe | Guide owner | Fixture region |
| --- | --- | --- |
| 16–23: DecimalMaxPlaces and exact Int64 conversion; migration of rule ID/message | [Decimal precision](../guides/validation.md#recipe-decimal-precision) | `httpdto/validation_recipes.go#decimal-precision`; migration requirements in owner prose |
| 24–26: correlated file/image kinds | [Upload kind owner](../guides/http-uploads.md#match-extension-and-content-together) | Explicit handoff to foundry-files/runtime lane; no duplicate inclusion or claim here |
| 27–32: typed country, Number/For, Parse and message definitions | [Normalize](../guides/phone.md#recipe-normalize-phone), [validate](../guides/phone.md#recipe-validate-phone), [messages](../guides/phone.md#recipe-phone-messages) | `codecs/phone.go#normalize-phone`, `#validate-phone`, `#phone-messages` |
| 33–37: generated selectors, All/Rules, Optional/Nullable and Bail | [Typed rules](../guides/validation.md#recipe-check-values) | `httpdto/validation_recipes.go#patch-rules` |
| 38–41: ValidateBody and Check outside HTTP | [HTTP rules](../guides/validation.md#recipe-compose-rules), [direct checks](../guides/validation.md#recipe-check-values) | `httpvalidation/validation.go#validate-http-body`; `httpdto/validation_recipes.go#check-update` |
| 42–46, 102–109: executor/query/key-expression lookup, cheap checks first and actor-scoped uniqueness | [Database rules](../guides/validation.md#recipe-database-rules) | Existing `goway/guideadapters/examples_test.go#database-rules`; `httpdto/validation_recipes.go#typed-validation-lookup` |
| 47–53: reusable Custom rule, typed translation and domain reuse | [Custom rule](../guides/validation.md#recipe-custom-rule) | `httpdto/validation_recipes.go#custom-translated-rule` |
| 49–51: generated message arguments and field-specific translation | [Translate a rule](../guides/validation-messages.md#recipe-translate-rule) | `localization/messages.go#rule-message-arguments`; `localization/validation.go#translated-field-rule`; `configured/catalog.go#localize-validation` |
| 57–62: generated field and enum labels | [Field labels](../guides/validation.md#recipe-field-labels), [enum labels](../guides/localization.md#recipe-feature-labels) | `clientcontracts/dto.go#client-field-label`; `localization/messages.go#enum-labels`; trim_type remains declaration reference prose |
| 63–72, 89–97: JSON catalogs, typed plural arguments, other form, framework definitions and locale parity | [Messages](../guides/localization.md#recipe-declare-messages), [catalogs](../guides/localization.md#recipe-load-catalogs), [configured messages](../guides/validation-messages.md#recipe-configure-messages) | `localization/messages.go#typed-plural-message`; `configured/catalog.go#format-cart-message`, `#load-message-catalog`, `#configured-catalog` |
| 65–67, 98–101: browser typed messages and frontend-only plural restriction | [Browser messages](../guides/localization.md#recipe-browser-messages) | `clientcontracts/testdata/messages.ts#browser-typed-messages`, `#browser-text-message`, `#browser-frontend-messages`; already registered in typescript-inputs.json |
| 73–77, 98–101: injected Services.Locales, RequestLocale and explicit background locale | [Request locale](../guides/localization.md#recipe-request-locale), [typed formatter](../guides/localization.md#recipe-declare-messages) | `configured/catalog.go#request-message`, `#format-cart-message` |

No owned recipe is unresolved. Correlated uploads are explicitly handed to the
runtime lane and are not claimed as extracted by this lane. The app's translator
wrapper names, catalog locations, locale choices, username policy, ownership
checks and execution instructions remain local. Framework message definitions
and native rule names are documented at their existing owners.

## Changes and ownership exceptions

All thirteen assigned guides are changed. New functions live in the owned
`authenticating`, `httpmiddleware`, `httpdto`, `configured`, `startupconfig` and
`codecs` packages. The new codecs package has a real phone implementation, not a
placeholder. No fixture annotations or generated outputs changed.

Marker-only additions outside the owned fixture packages reuse the existing
compiled declaration or scenario; duplicating those in an owned package would
create a second recipe or generated owner:

| File | Reason |
| --- | --- |
| `tests/fixtures/consumer/passwords/login.go` | Existing complete password model and login callbacks; authenticating already delegates trusted password verification |
| `tests/fixtures/consumer/localization/messages.go` | Existing generated message argument and enum declarations |
| `tests/fixtures/consumer/localization/validation.go` | Existing translated field rule uses those generated messages |
| `tests/fixtures/consumer/httpvalidation/validation.go` | Existing ValidateBody wiring to the generated endpoint; moving it into httpdto would create an import cycle |
| `tests/fixtures/consumer/idempotenthttp/application.go` | Existing complete authenticated idempotency/resource/outbox scenario |
| `tests/fixtures/consumer/clientcontracts/dto.go` | Existing generated query label/kind declaration, with matching client export |
| `tests/fixtures/consumer/clientcontracts/testdata/messages.ts` | Existing registered browser message typing input |

The remaining changed-file inventory follows. Entries above are included once in
the complete list below; generated files, tooling and framework source are absent.
- [docs/evidence/guide-recipes-http-config-20261107.md](../../docs/evidence/guide-recipes-http-config-20261107.md)
- [docs/guides/authentication.md](../../docs/guides/authentication.md)
- [docs/guides/generated-configuration.md](../../docs/guides/generated-configuration.md)
- [docs/guides/http-middleware.md](../../docs/guides/http-middleware.md)
- [docs/guides/http-model-binding.md](../../docs/guides/http-model-binding.md)
- [docs/guides/http-presence.md](../../docs/guides/http-presence.md)
- [docs/guides/idempotent-operations.md](../../docs/guides/idempotent-operations.md)
- [docs/guides/localization.md](../../docs/guides/localization.md)
- [docs/guides/named-services.md](../../docs/guides/named-services.md)
- [docs/guides/phone.md](../../docs/guides/phone.md)
- [docs/guides/settings.md](../../docs/guides/settings.md)
- [docs/guides/tokens.md](../../docs/guides/tokens.md)
- [docs/guides/validation-messages.md](../../docs/guides/validation-messages.md)
- [docs/guides/validation.md](../../docs/guides/validation.md)
- [tests/fixtures/consumer/authenticating/access_scopes.go](../../tests/fixtures/consumer/authenticating/access_scopes.go)
- [tests/fixtures/consumer/authenticating/accounts.go](../../tests/fixtures/consumer/authenticating/accounts.go)
- [tests/fixtures/consumer/authenticating/browser_tokens.go](../../tests/fixtures/consumer/authenticating/browser_tokens.go)
- [tests/fixtures/consumer/authenticating/credential_stores.go](../../tests/fixtures/consumer/authenticating/credential_stores.go)
- [tests/fixtures/consumer/authenticating/guide_routes.go](../../tests/fixtures/consumer/authenticating/guide_routes.go)
- [tests/fixtures/consumer/authenticating/permissions.go](../../tests/fixtures/consumer/authenticating/permissions.go)
- [tests/fixtures/consumer/authenticating/token_routes.go](../../tests/fixtures/consumer/authenticating/token_routes.go)
- [tests/fixtures/consumer/authenticating/tokens.go](../../tests/fixtures/consumer/authenticating/tokens.go)
- [tests/fixtures/consumer/clientcontracts/dto.go](../../tests/fixtures/consumer/clientcontracts/dto.go)
- [tests/fixtures/consumer/clientcontracts/testdata/messages.ts](../../tests/fixtures/consumer/clientcontracts/testdata/messages.ts)
- [tests/fixtures/consumer/codecs/phone.go](../../tests/fixtures/consumer/codecs/phone.go)
- [tests/fixtures/consumer/configured/catalog.go](../../tests/fixtures/consumer/configured/catalog.go)
- [tests/fixtures/consumer/configured/runtime_settings.go](../../tests/fixtures/consumer/configured/runtime_settings.go)
- [tests/fixtures/consumer/configured/settings.go](../../tests/fixtures/consumer/configured/settings.go)
- [tests/fixtures/consumer/httpdto/dto.go](../../tests/fixtures/consumer/httpdto/dto.go)
- [tests/fixtures/consumer/httpdto/validation_recipes.go](../../tests/fixtures/consumer/httpdto/validation_recipes.go)
- [tests/fixtures/consumer/httpmiddleware/router.go](../../tests/fixtures/consumer/httpmiddleware/router.go)
- [tests/fixtures/consumer/httpquery/query.go](../../tests/fixtures/consumer/httpquery/query.go)
- [tests/fixtures/consumer/httpvalidation/validation.go](../../tests/fixtures/consumer/httpvalidation/validation.go)
- [tests/fixtures/consumer/idempotenthttp/application.go](../../tests/fixtures/consumer/idempotenthttp/application.go)
- [tests/fixtures/consumer/localization/messages.go](../../tests/fixtures/consumer/localization/messages.go)
- [tests/fixtures/consumer/localization/validation.go](../../tests/fixtures/consumer/localization/validation.go)
- [tests/fixtures/consumer/passwords/login.go](../../tests/fixtures/consumer/passwords/login.go)
- [tests/fixtures/consumer/startupconfig/load.go](../../tests/fixtures/consumer/startupconfig/load.go)
- [tests/fixtures/consumer/startupconfig/settings.go](../../tests/fixtures/consumer/startupconfig/settings.go)

## Lead commands

Render the owned guides first, using the existing native Go toolchain and cache:

```sh
go run ./internal/cmd/renderdocs \
  -guide authentication.md -guide tokens.md -guide http-middleware.md \
  -guide idempotent-operations.md -guide http-model-binding.md \
  -guide http-presence.md -guide generated-configuration.md \
  -guide named-services.md -guide settings.md -guide validation.md \
  -guide validation-messages.md -guide localization.md -guide phone.md
make docs-check
```

For the lead's combined milestone checkpoint, `make fixture-check` compiles and
exercises the independent consumers, records actual Go input membership for
checkdocs, and requires the registered TypeScript proof. Supply the existing
service configuration and native Node/TypeScript environment. If the combined
checkpoint uses `make verify`, do not repeat fixture-check separately.

For a selective Go checkpoint instead, the changed package set is:

```sh
(cd tests/fixtures/consumer && go test ./authenticating ./httpmiddleware ./httpdto ./httpquery ./configured ./startupconfig ./codecs ./localization ./passwords ./httpvalidation ./idempotenthttp)
```

The clientcontracts edits are marker-only; use its existing required TypeScript
checkpoint to prove `messages.ts` remains a registered compiler input. No new
TypeScript source or registration was added. Existing goway regions are reused
without changing their source or prior test results. Renderer output and these
checks must pass before removing duplicated teaching from application skills.
