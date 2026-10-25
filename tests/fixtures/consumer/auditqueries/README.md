# Audit query fixture

`Accounts` and `Labels` declare ORM models. `AccountIdentity` and `LabelIdentity`
retain their stored key codecs. `ORMDomain()` registers instance-owned ORM audit
observers with `AccountPolicy` and `LabelPolicy`.

Tests keep the original audit assertions: stored mutation output, automatic credential
and JSON redaction, excluded notes, exact decimals, natural-key identity, omitted
untouched fields, historical readers, soft delete/restore, and rollback together
with the model. `ORMEmailAfter` reads stored history explicitly. Presentation methods
remain opt-in. Set-based writes acknowledge skipped model hooks; `orm.UpdateEach`
retains per-model auditing. The unused legacy `Domain` and `EmailAfter` helpers have
been removed. Model directives, generated output and its `accountHooks` callback
remain until the lead's serial integration barrier.
