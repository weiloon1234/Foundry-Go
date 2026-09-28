# Request-aware validation messages — accepted design

Status: implemented and accepted on 2026-09-26. See the
[usage guide](validation-messages.md) and
[current-source acceptance evidence](../evidence/validation-messages-20260926.json).
The design below is retained as the implemented acceptance scope.

## Intended experience

A request selects its supported locale through the existing `http.Locale`
middleware. Failed rules carry stable codes, typed declaration parameters and
field labels. A configured response presenter resolves those messages through
the application's immutable `i18n.Catalog` before sending the ordinary error DTO.
Applications get useful English messages automatically and can supply their own
translations and field-specific wording. Handlers should not repeat translation
calls or build message dictionaries.

Proposed flow:

`request locale → typed request validation → message resolution → existing 422 response`

The same presenter should cover body, query, path, form and upload diagnostics,
including nested collection paths. Binding/decoding failures retain their existing
400 status; business-rule failures remain 422. This is presentation, not a change
to validation, authorization, field selection or handler admission.

## Reuse existing owners

- Keep `http.Locale` and `i18n.RequestLocale` as locale selection. An explicit
  supported request preference wins, then `Accept-Language`, then the configured
  application locale. No mutable global locale and no new service bag in context.
- Reuse `i18n.Catalog`, typed `i18n/message.Message[A]`, generated message argument
  declarations and current fallback/pluralization support. Keep the catalog
  injected into an application/router-owned presenter.
- Give every built-in rule one canonical message definition and English fallback.
  Its rule ID, supported arguments and defaults have one owner. Rule constructors,
  response rendering, inspection and client metadata reuse that definition.
- Retain each issue's message recipe alongside its safe diagnostic data. Do not
  reconstruct it from an English sentence or parse a JSON Pointer to guess a rule.
  Translate an owned diagnostic copy; concurrent requests never mutate a rule,
  catalog or shared error.

## Messages and overrides

Use labels and typed bounds to produce useful messages such as “Email address
must contain at most 254 characters”, with the catalog choosing plural forms.
Carry only approved declaration arguments: field label, minimum/maximum, size,
related-field label and safe configured choices. Never interpolate submitted
passwords, tokens, arbitrary rejected objects, upstream errors, SQL or scope values.

Preserve `WithMessage` as an explicit literal override, with no template parsing.
Add a typed translated-message override using the existing message machinery;
custom rules get the same capability. Field-specific overrides attach to typed
field/rule declarations rather than string paths in an untyped global dictionary.

Proposed precedence is an explicit field/rule override, then an application catalog
override for that built-in message, then the built-in default. Locale lookup uses
the requested locale and configured fallback; approved English text remains the
last fallback even when English is not an enabled request locale. Missing optional
translations must not turn an ordinary validation rejection into a server error.
Malformed templates, undeclared placeholders and argument-type mismatches should
fail generation or startup validation, wherever the information is available.

## Compatibility and non-HTTP callers

Keep status, `error_code`, issue `code`, JSON Pointer `path`, issue ordering and
truncation behavior stable. Human-facing `message` and `label` may vary by locale.
Use the same safe error-classification path; infrastructure faults never become
translated field rejections. If response locale varies by `Accept-Language`, set
the appropriate response language/cache metadata at the response boundary.

Direct `Rule.Check` remains usable without an HTTP request or catalog, returning
English fallback diagnostics. Add explicit localization/presentation for CLI,
workers and other callers, extending the current `Errors.LocalizeLabels` path.
Configured application assembly should connect its locale/catalog service to the
presenter once. Direct router construction remains available for advanced users.

Browser validators should consume the same exported rule/message definitions,
not another manually maintained rule-message table. Preserve explicit incomplete
reports for server-only validation. Translation must not suggest that a database
or custom server check has executed in the browser.

## Implementation order and acceptance

1. Define the shared message recipe, typed parameters, built-in defaults and
   explicit override contracts. Migrate built-in static strings at their source.
2. Add catalog-backed diagnostic presentation, label/related-label translation,
   fallback behavior and the explicit non-HTTP API.
3. Connect configured HTTP presentation to request locale across all request
   sources, keeping existing response codes and safe error handling.
4. Update inspection/client metadata, independent consumer examples and guides.
   Prove any generated API with compiler negatives and actual editor probes.

Acceptance should exercise concurrent requests with different locales, nested
fields and index paths, pluralization and numeric bounds, custom rule messages,
per-field overrides, omitted locales, missing translations, invalid templates,
raw `WithMessage` literals, all request sources, async/model rules, and secret-safe
500 responses. Run the complete repository gate, required integrations and relevant
races. Do not count example catalogs alone as request-to-response integration.
