# Encrypted entries

`Entries` uses `orm.Encrypted` and `orm.NullableEncrypted` columns. Setters accept
`encrypted.Text` and `encrypted.JSON[Settings]`; randomized fields have no ordering,
equality or incoming-conflict capability. Omitted fields retain their envelopes.

The existing PostgreSQL assertions exercise row-bound encryption, nulls, bulk
inserts, copied-ciphertext rejection, key rotation and missing-key failures through
ORM calls. Schema, wire values and generated declarations remain unchanged.
