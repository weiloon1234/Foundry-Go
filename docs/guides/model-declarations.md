# Typed model declarations and write roles

Models are ordinary Go structs. An `orm.Define` declaration binds their persisted fields, typed primary key and optional lifecycle configuration. Register tables with `application.WithTables` so declaration validation happens before resources start. `orm.Check` validates declarations directly; query execution also rejects invalid declarations.

<!-- orm-example: roles-after -->
```go
type EmailInput string
type User struct {
	ID        int64
	Email     string
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt value.Nullable[time.Time]
}

func (User) MutateEmail(input EmailInput) (string, error) {
	return strings.ToLower(strings.TrimSpace(string(input))), nil
}

type UserTable struct {
	orm.Table[User, int64]
	Primary   orm.Primary[User, int64]
	Email     orm.InputText[User, EmailInput, string]
	CreatedAt orm.CreatedAt[User, time.Time]
	UpdatedAt orm.UpdatedAt[User, time.Time]
	DeletedAt orm.SoftDelete[User, time.Time]
	Soft      orm.SoftDeletable[User]
}

var Users = orm.Define("users", func(b orm.Builder[User], u *User) UserTable {
	table := orm.TableOf(b, &u.ID)
	deleted := orm.SoftDeleteOf(b, &u.DeletedAt)
	return UserTable{table, orm.PrimaryOf(b, table.Key()),
		orm.InputTextOf[User, EmailInput](b, &u.Email, nil),
		orm.CreatedAtOf(b, &u.CreatedAt), orm.UpdatedAtOf(b, &u.UpdatedAt),
		deleted, orm.SoftDeletes(deleted)}
})
```

The example and its imports are in the [compiled migration package](../../tests/fixtures/goway/migration/examples_test.go). Struct tags still declare column names, database-default omission and ignored fields. Migrations own persisted schema. This declaration creates no SQL tables and adds no response DTO or wire fields.

## Select the field's role

| Field | Declaration | Write contract |
| --- | --- | --- |
| Ordinary stored value | `ColumnOf`, `TextOf`, `NumberOf`, `TimeOf` or the matching family | `Set` accepts the stored type. |
| Go-mutated value | `Input*Of[M, Input](b, pointer, transform)` | `Set` accepts the input type; predicates use the stored type. |
| Managed creation time | `CreatedAtOf` and `CreatedAt[M, V]` | Preserves a supplied creation time; eligible for insert-select mapping. |
| Managed update time | `UpdatedAtOf` and `UpdatedAt[M, V]` | Write conventions supply it; SQL source mapping is unavailable. |
| Managed deletion time | `SoftDeleteOf` and `SoftDelete[M, V]` | Retains nullable stored-time reads and deletion conventions. |
| Primary lookup key | `TableOf(b, &model.ID).Key()` | Concrete stored key for lookup and predicates. |
| Primary insert-select target | `PrimaryOf(b, table.Key())` | Explicitly enables SQL insertion into an unmutated primary key. |
| Mutated primary input | `InputKeyOf` | Accepts typed key input; stored-key lookup remains unchanged. |

`PrimaryOf` is needed only when an application maps a primary key in insert-select. Ordinary models need only `TableOf` and its key. `PrimaryOf` and `InputKeyOf` are mutually exclusive. `NumberKey`, `TextKey` and key adapters preserve the primary role; they cannot turn a key into an ordinary update target.

Managed time carriers support ordinary time predicates, ordering and expressions. Declaring a managed pair through `TimeOf` fails startup with the field name and required carrier. `NoTimestamps()` opts out when both fields use ordinary declarations; it conflicts with explicit managed roles. `NoSoftDeletes()` similarly conflicts with `SoftDeleteOf`.

Every Go-mutated field requires an `Input*` carrier, including same-type transformations. Its family preserves stored capabilities: an input enum cannot become an ordered numeric column through conversion. Startup rejects orphan/invalid `Mutate<Field>` and `Access<Field>` methods, missing transformations and conflicting explicit/discovered transformations. See [mutators](model-mutators.md).

## Soft-delete capability

Expose `orm.SoftDeletes(deletedColumn)` as an ordinary table field such as `Users.Soft`. Force deletion and bounded restoration require that exact declaration's token. Missing or wrong-model tokens fail compilation; zero or foreign-declaration tokens fail validation before SQL. The token does not change query visibility or authorize tenant access.

<!-- orm-example: force-after -->
```go
func forceDelete(ctx context.Context, tx *database.Tx, id int64) error {
	_, err := orm.ForceDelete(ctx, tx, Users.Query().WithTrashed(), Users.Soft, Users.Key(), id)
	return err
}
```

## SQL assignment constraints

`rows.MapInsert` requires an `orm.InsertTarget`. `rows.MapUpdate` requires an `orm.UpdateTarget`. Input columns and managed update timestamps cannot satisfy either target. Primary columns satisfy insertion only when declared through `PrimaryOf`; they never satisfy update. A lookup-only `KeyColumn` cannot erase those restrictions. Separate source-match, insert-mapping and update-mapping types retain their phase and nullability.

Ordinary source reads, joins, projections and aggregates retain the stored field capabilities. These roles restrict assignment paths without changing the database schema or serialized contracts.

For the old-to-new API mapping, see [the ORM migration guide](orm-migration.md).
