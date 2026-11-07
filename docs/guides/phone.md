# Normalize and validate phone numbers

<a id="agent-entry"></a>
## Agent entry

- [Normalize accepted input](#recipe-normalize-phone)
- [Validate typed phone fields](#recipe-validate-phone)
- [Register phone messages](#recipe-phone-messages)

<a id="recipe-normalize-phone"></a>
## Normalize accepted input

Use `phone.Parse` with the selected `countries.Code` before storing a contact number:

<!-- compiled-source: tests/fixtures/consumer/codecs/phone.go#normalize-phone -->
```go
```

Import `github.com/weiloon1234/Foundry-Go/phone` and
`github.com/weiloon1234/Foundry-Go/countries`. Store the selected country separately
from the canonical string. The returned `phone.E164` has private storage;
its zero value is absent. Use `String()` at a persistence or transport boundary.

The parser accepts national and international input, trims surrounding whitespace,
and validates against the selected region. A Singapore number with `MY` rejects,
even if the number is valid in Singapore. Applications still choose which
countries they allow. The parser does not check number ownership or reachability.

Input exceeding `phone.MaxInputBytes` (100 bytes) rejects before trimming.
Empty or whitespace-only input rejects. Errors match `fault.Invalid` and contain
no submitted input. `phone.Options{}` rejects extensions, because E.164 cannot
retain them. To explicitly discard an extension, use
`phone.Options{Extensions: phone.DiscardExtensions}` for both parsing and validation.
Keep the approved `github.com/nyaruka/phonenumbers/v2 v2.0.15` metadata version
aligned across both operations; metadata upgrades can change accepted numbers.
Only `phone` and its importers compile this dependency's metadata.

<a id="recipe-validate-phone"></a>
## Declare validation rules

Import `github.com/weiloon1234/Foundry-Go/validation/phone` as `phonevalidation`.
For a fixed country, bind `phonevalidation.Number[string](country, options)`
to an ordinary typed validation field. Named string types are also supported.

For a contact with its own selected country, use typed fields:

<!-- compiled-source: tests/fixtures/consumer/codecs/phone.go#validate-phone -->
```go
```

Use generated validation fields when your DTO has them. `For` reports failures
at the number field's path and retains its label. The selected region belongs to
each validation check, so a declared rule can serve concurrent requests.
Validation calls `phone.Parse` and leaves the input unchanged. Normalize accepted
input separately with the same options before storing it.

Keep contact optionality in your declarations. For a patch field of type
`value.Optional[value.Nullable[Contact]]`, use
`validation.Optional(validation.Nullable(rule))`. Omission and explicit null
skip the phone check. A supplied contact containing an empty number rejects.
For an optional number with a fixed region, wrap `Number` in the same way.
Applications that permit empty text must declare that condition explicitly;
empty text does not imply omission or null.

<a id="recipe-phone-messages"></a>
## Translate phone messages

<!-- compiled-source: tests/fixtures/consumer/codecs/phone.go#phone-messages -->
```go
```

Register `phonevalidation.MessageDefinitions()` alongside your ordinary
`validation.MessageDefinitions()` when adding phone translations to your catalog.
The `validation.phone_without_extension` key has the English fallback:

> Enter a valid phone number for the selected country, without an extension.

Explicit extension discarding uses `validation.phone` with this fallback:

> Enter a valid phone number for the selected country.

Both signatures accept the text parameter `attribute`, which receives the
bound field label. Applications retain their own message declarations: compose
`validation.Custom` and `validation.WithTranslation` around `phone.Parse` to keep
an existing application rule ID, translation key, or literal message.

Client metadata marks these rules as server-only and retains the message recipe,
100-byte limit, and extension policy (`0` rejects, `1` discards). Clients must
report the skipped server check rather than substitute a regular expression.
The default rule ID is `phone.number`; explicit extension discarding uses
`phone.number_discard_extension`. Rules do not export submitted numbers or the
per-check region. See [validation messages](validation-messages.md) for catalog
configuration and [typed validation](validation.md) for presence wrappers.
