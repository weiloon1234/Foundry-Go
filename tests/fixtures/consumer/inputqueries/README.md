# Distinct mutation-input fixture

`Members.Email.Set` accepts `EmailInput` and stores `StoredEmail`; `Members.Note.Set`
accepts `LabelInput` and stores `Nullable[StoredLabel]`. Comparisons use stored
values. `Member.AccessEmail` explicitly produces the unchanged `EmailLink` type.

Before hooks use `orm.WriteGet` and `orm.WriteNullableGet` with these setters, so
omitted, NULL and present inputs retain their distinct types. The declaration's
local hooks capture persisted results in `State.ORMCaptured`. Assignment formatting
is redacted.

All five mutation-input compiler cases exercise the ORM surface. Legacy hook
state and the model-generation directive are removed.

Live before-write callbacks can call `orm.WriteUnset(write, Members.Note.Set)` to
restore omission, independently of SQL NULL and an explicit zero input. The fixture
keeps typed getter and formatting assertions inside that callback lifetime. Ordinary
CRUD and conflicts now use ORM inputs; raw stored literals still cannot bypass a
Go mutator through a SQL conflict expression.
