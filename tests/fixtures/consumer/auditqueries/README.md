# Audit query fixture

`Accounts` and `Labels` declare ORM models. `AccountIdentity` and `LabelIdentity`
retain their stored key codecs. `ORMDomain()` registers instance-owned ORM audit
observers with `AccountPolicy` and `LabelPolicy`; do not also register legacy `Domain()`.

Tests keep the original audit assertions: stored mutation output, automatic credential
and JSON redaction, excluded notes, exact decimals, natural-key identity, omitted
untouched fields, historical readers, soft delete/restore, and rollback together
with the model. `ORMEmailAfter` reads stored history explicitly. Presentation methods
remain opt-in. Set-based writes acknowledge skipped model hooks; `orm.UpdateEach`
retains per-model auditing. Generated output remains for the integration barrier.
