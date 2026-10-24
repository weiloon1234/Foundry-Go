# Distinct mutation-input fixture

`Members.Email.Set` accepts `EmailInput` and stores `StoredEmail`; `Members.Note.Set`
accepts `LabelInput` and stores `Nullable[StoredLabel]`. Comparisons use stored
values. `Member.AccessEmail` explicitly produces the unchanged `EmailLink` type.

Before hooks use `orm.WriteGet` and `orm.WriteNullableGet` with these setters, so
omitted, NULL and present inputs retain their distinct types. The declaration's
local hooks capture persisted results in `State.ORMCaptured`; the generated suite
keeps `State.Captured` during coexistence. Assignment formatting is redacted.

All five mutation-input compiler cases now exercise the ORM surface. Generated
outputs and directives are retained until dependent cutovers and lead regeneration.
