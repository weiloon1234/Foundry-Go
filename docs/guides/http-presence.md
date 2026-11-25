# Presence through JSON and query transport

<a id="agent-entry"></a>
## Agent entry

- [Declare PATCH and query inputs](#recipe-patch-query)
- Declare: [Omission, null and supplied values](#recipe-presence)
- Compose: [Endpoint preparation](http-endpoints.md#recipe-compose-endpoint)

<a id="recipe-presence"></a>
## Declare presence and rules

Use generated DTO fields with `Optional[T]` to distinguish omission from a
supplied value. Use `Optional[Nullable[T]]` when explicit JSON null is valid.
Rules inspect presence and content without trimming, replacing or unsetting
the value delivered to a domain handler.

| Decoded input | Present | Required | Prohibited | Absent |
| --- | --- | --- | --- | --- |
| Omitted | Reject | Reject | Accept | Accept |
| Explicit null, on a nullable field | Accept | Reject | Accept | Reject |
| Empty or whitespace-only text | Accept | Reject | Accept | Reject |
| Empty collection | Accept | Reject | Accept | Reject |
| Supplied numeric zero or false | Accept | Accept | Reject | Reject |
| Supplied nonempty value | Accept | Accept | Reject | Reject |

`RequiredNullable` and `ProhibitedNullable` handle the nullable rows.
`Present` and `Absent` retain the concrete wrapped type. Collection presence
does not validate each element: `[""]` has one item and requires a separate
element rule to reject its blank content.

Framework imports from `github.com/weiloon1234/Foundry-Go`: `model`, `validation`, `value`.

`PresenceInputValidationFields` is generated for the DTO by `foundry generate`. A DTO and its rules reuse the generated JSON and validation declarations:

<!-- compiled-source: tests/fixtures/consumer/validationrules/presence.go#presence-input-rules -->
```go
//foundry:dto http=body
type PresenceInput struct {
	Label     value.Optional[string]                 `json:"label,omitzero"`
	Nickname  value.Optional[value.Nullable[string]] `json:"nickname,omitzero"`
	Count     value.Optional[int]                    `json:"count,omitzero"`
	Active    value.Optional[bool]                   `json:"active,omitzero"`
	Forbidden value.Optional[value.Nullable[string]] `json:"forbidden,omitzero"`
	Tags      value.Optional[[]string]               `json:"tags,omitzero"`
}

func PresenceRules() validation.Rule[PresenceInput] {
	fields := PresenceInputValidationFields()
	return validation.All(
		validation.Rules(fields.Label, validation.Required(validation.MaxLength[string](40))),
		validation.Rules(fields.Nickname, validation.RequiredNullable(validation.MinLength[string](2))),
		validation.Rules(fields.Count, validation.Required(validation.Min(0))),
		validation.Rules(fields.Active, validation.Required[bool]()),
		validation.Rules(fields.Forbidden, validation.ProhibitedNullable[string]()),
		validation.Rules(fields.Tags, validation.Required(validation.Each[[]string](validation.NonBlank[string]()))),
	)
}
```

Query input uses the same Optional presence semantics, with the representations
query strings actually support. `label` and `label=` supply an empty string;
`label=null` supplies the literal text "null". Query transport does not invent a
JSON null spelling. `count=0` and `active=false` remain supplied values.
Null, malformed numeric/boolean representations, duplicate scalar parameters
and unknown fields are transport failures when the descriptor rejects them.

JSON null requires an explicit nullable contract. For example, null assigned
to `Optional[int]` fails decoding with HTTP 400 before presence rules run.
A well-formed decoded value that fails a declared rule produces HTTP 422.
Neither kind of rejection reaches the domain handler. Unknown submitted field
names are redacted from errors; declared field paths remain useful.

Query and body values remain separate in `Input.Path`, `Input.Query` and
`Input.Body`. A query field cannot satisfy a body field's required rule, even
when they share a name. Body-based conditional rules activate from supplied
body zero/false values. Generated field references retain that source through
the endpoint's validation adapter; errors use paths such as `/body/label`,
`/query/label` and `/body/tags/1`.

This guide covers JSON and query transport. [Multipart presence and upload
cleanup](http-uploads.md) are also delivered.

<a id="recipe-patch-query"></a>
## Declare PATCH and query inputs

A PATCH DTO keeps omission separate from an explicit null or supplied value.
Omitted fields leave stored values unchanged; map supplied values into the
[typed write API](model-writes.md#recipe-write-models).

The application owns `models.User` and `models.Status` (a string-backed `//foundry:enum`) and any generated tables or identities used below.

<!-- compiled-source: tests/fixtures/consumer/httpdto/dto.go#patch-presence -->
```go
//foundry:dto http=body
type UpdateUser struct {
	Email    value.Optional[string]                 `json:"email,omitzero"`
	Nickname value.Optional[value.Nullable[string]] `json:"nickname,omitzero"`
	State    value.Optional[models.Status]          `json:"state,omitzero"`
}
```

Query descriptors retain the same omission type but use query-string values:

<!-- compiled-source: tests/fixtures/consumer/httpquery/query.go#query-presence -->
```go
//foundry:query
type SearchInput struct {
	User     model.ID[models.User]
	Search   value.Optional[string] `query:"q"`
	Statuses []models.Status        `query:"status"`
}
```

Pointers and collection encoding follow [DTO JSON contracts](http-dtos.md).
Choose their tags for the intended wire contract; an empty collection is not
an omitted field merely because both have zero elements.
