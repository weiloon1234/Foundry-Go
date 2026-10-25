# Tenant query fixture

`Documents`, `Tags` and `DocumentTags` publish ORM declarations. Document scopes
attach lazily and obtain the tenant from the executing context. Missing context
fails closed. `ORMTenantScope` and `ORMPublishedScope` identify explicit query-local
bypasses; `orm.WithoutGlobalScope` and `orm.WithoutGlobalScopes` leave the declaration
unchanged. Inserts do not filter their input.

`DocumentLinks` and `TagLinks` retain target and pivot ownership. Tests keep tenant
isolation through reads, relations, scoped upserts, set writes, lookup defaults,
concurrent pivot synchronization and bounded pruning. `prune.ORMSoftModel` requires
the declared soft-delete token for physical pruning. Legacy scope/relation
factories and model-generation directives are removed.
