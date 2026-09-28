# C02 — Named services and defaults

Prerequisite: C01. This milestone owns named/default selection and configured
construction for PostgreSQL, Redis, storage and cache.

## Selection and configuration

- Keep distinct typed names for database connections, Redis connections, disks
  and cache stores. Reuse existing disk declarations and registry rather than
  replacing them. Go declarations/constants give IDE completion; external names
  are parsed and membership checked during assembly.
- Every enabled service family has a default referring to an existing instance.
  Default access and named access return the same concrete object. Missing names
  never fall back; no first-map-entry selection or duplicate default pool.
- Duplicate names, absent defaults, unsupported drivers, invalid settings,
  missing referenced connections and cycles fail before boot. Limits bound names,
  memory, pool totals, file inputs and resulting resource use.
- Framework-owned serializable settings describe adapters without leaking native
  SDK clients. Convert those settings to existing runtime configs in one place.
  Generated schemas and explicit typed overrides use the same fields and rules.
- Support multiple entries with the same driver and different settings. Named
  config collections need well-defined nested duration/secret decoding and
  default application; extend the shared config codec if opaque JSON is inadequate.

## PostgreSQL and Redis

PostgreSQL connections retain their own optional primary/read topology, pool
bounds, observations and lifecycle. Preserve executor identity throughout a
transaction; default selection cannot switch it. Cross-connection joins and
distributed atomic commits are not implied. Tooling targets the default or one
explicit connection; no migrations run at normal boot.

Redis connections are shared by explicitly referencing services where configured.
Their borrowers close before their owner. Preserve namespaces and the distinction
between Redis databases and pub/sub isolation. Do not create duplicate clients
for cache/jobs using the same configured connection.

## Storage and credentials

Configure local, AWS S3 and R2 disks through the shared storage contract. Local
storage retains its managed object format. Upload/image/download consumers use
the same disk API for each driver. A default disk needs no name at ordinary calls.

Provide framework-owned credential settings and an extensible typed provider
boundary shared by S3/R2/SES. Preserve secrets, rotating credentials, default AWS
credential-chain behavior and cancellation. Ordinary consumers require no AWS
SDK imports; native customization remains an explicit advanced boundary.

## Cache

Configure memory, Redis, file and PostgreSQL stores through the existing typed
cache contract. New backends implement the shared contract suites and declare
capabilities honestly. Required tags, counters, atomic operations and fill
coordination must be checked before serving requests. Local-only coordination
must not be presented as distributed guarantees.

File cache confines all operations to an owned root, handles process concurrency,
atomic publication, expiry and bounded cleanup. PostgreSQL cache contributes
explicit migrations, atomic expiry-aware operations and bounded pruning. No
destructive database setup or new database engine.

## Verification

Prove default/named identity, two same-driver configurations, startup failure
without leaked resources, reverse cleanup, concurrent use, independent apps,
driver switches without domain-code changes, real PostgreSQL/Redis isolation,
file process concurrency and cloud protocol compatibility. Reuse prior live
provider evidence only when relevant provider behavior is unchanged; record that
limit. Complete native full and relevant race/consumer/editor gates.
