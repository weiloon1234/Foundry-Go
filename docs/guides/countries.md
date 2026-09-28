# Countries

Milestone 18 passed native verification and consumer review. The
[master evidence](../../blueprint/00-master-architecture-and-parity.md#milestone-18-verification-and-consumer-review)
records the checks and operational limits.

`countries.Country` is an ordinary generated model with the typed
natural primary key `countries.Code`. Reference data is installed only through an
explicit seeder invocation.

```go
// Register countries.Migrations() in the ordinary migration registry first.
seeders, err := seed.New(countries.Seeder(store))
result, err := seeders.Run(ctx, database, countries.SeederID)

code, err := countries.ParseCode("my")
malaysia, err := countries.Find(ctx, store, code)
```

The seeder uses the [extension store](model-extensions.md#shared-owners)'s schema
and the existing database seeder transaction. `countries.Seed` opens its own
transaction; `SeedIn` joins a business transaction through a savepoint and returns
a provisional result until the caller commits. Each invocation applies one
bounded atomic upsert batch. No table reset, boot hook or network download occurs.

The bundled version is `foundry-countries-v1`, with 250 entries and the pinned
IANA tzdb 2026a country/zone mapping. See the [data provenance](../../countries/data/README.md)
for input hashes and reference details. This is a versioned snapshot, not a live
geographic or currency feed. Named zones resolve through Go's configured timezone
database; unavailable zone names fail seeding before writes.

New countries start disabled, with no conversion rate and `IsDefault=false`.
Repeated seeding refreshes reference fields and the reference version, while
preserving existing `Status`, `ConversionRate`, `IsDefault` and creation time.
Conversion rates use the framework's exact decimal type. A failed upsert leaves
no partial batch; failed parent transactions roll back the seeder as well.

`Find`, `Exists`, `All` and `Enabled` borrow the store. `All` and `Enabled` sort by
name and code and are bounded to 1000 rows. Generated `QueryFoundryCountries`,
`CountryFields` and `CountryDraft` provide normal typed queries and administrative
updates. Currency and timezone collections use typed immutable JSON snapshots;
decode them explicitly to obtain fresh slices. Activation status is a generated
enum, and optional fields retain the distinction between null and zero.
