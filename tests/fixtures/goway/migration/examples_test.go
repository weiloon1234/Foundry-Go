package migration_test

import (
	"context"
	"errors"
	"strings"
	"time"

	"foundry.test/goway/coremodels"
	"foundry.test/goway/models"
	hooks "foundry.test/goway/models/hooks"
	"foundry.test/goway/models/mutators"
	reads "foundry.test/goway/models/reads"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"

	"github.com/weiloon1234/Foundry-Go/database/seed"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

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

// doc-example:begin insert-after
func insert(ctx context.Context, tx *database.Tx) error {
	_, err := orm.Insert(ctx, tx, models.Users.Table, orm.Set(models.Users.Email, "ada@example.test"))
	return err
}

// doc-example:end insert-after

// doc-example:begin force-delete
func forceDelete(ctx context.Context, tx *database.Tx, id int64) error {
	_, err := orm.ForceDelete(ctx, tx, Users.Query().WithTrashed(), Users.Soft, Users.Key(), id)
	return err
}

// doc-example:end force-delete

// doc-example:begin unset-assignment
func unset(draft *orm.Write[hooks.Member]) error {
	return orm.WriteUnset(draft, func() func(arg0 coremodels.LabelInput) orm.Assignment[hooks.Member] {
		receiver := hooks.Members.Note
		return func(arg0 coremodels.LabelInput) orm.Assignment[hooks.Member] {
			return orm.Set(receiver, arg0)
		}
	}())
}

// doc-example:end unset-assignment

// doc-example:begin read-field-change
func changes(changes orm.Changes[hooks.Member]) (bool, error) {
	field, err := orm.ChangeOf(changes, hooks.Members.Name)
	return field.Changed(), err
}

// doc-example:end read-field-change

// doc-example:begin observers-after
func observer() (lifecycle.Declaration, error) {
	return orm.NewObserver[hooks.Member]("members").Declare(memberHooks)
}

// doc-example:end observers-after

// doc-example:begin retrieved-hook
func retrieval() orm.RetrievalHooks[reads.Member] {
	return orm.RetrievalHooks[reads.Member]{Retrieved: func(context.Context, database.Executor, reads.Member) error { return nil }}
}

// doc-example:end retrieved-hook

// doc-example:begin tenant-global-scope
func scope(t reads.MemberTable) orm.GlobalScope[reads.Member] {
	return orm.Scope(t.Table, "tenant", func(ctx context.Context) (orm.Predicate[reads.Member], error) {
		id, err := tenantID(ctx)
		return orm.Eq(t.TenantID, id), err
	})
}

// doc-example:end tenant-global-scope

type tenantKey struct{}

func tenantID(ctx context.Context) (int64, error) {
	id, ok := ctx.Value(tenantKey{}).(int64)
	if !ok {
		return 0, errors.New("tenant required")
	}
	return id, nil
}

// doc-example:begin bypass-global-scope
func bypass() orm.Query[reads.Member] {
	return orm.WithoutGlobalScope(reads.Members.Query(), reads.MemberTenant(reads.Members))
}

// doc-example:end bypass-global-scope

// doc-example:begin set-write-without-hooks
func setWrite(ctx context.Context, tx *database.Tx) error {
	_, err := orm.UpdateAll(ctx, tx, orm.WithoutModelHooks(hooks.Members.Query()), orm.Set(hooks.Members.Name, "guest"))
	return err
}

// doc-example:end set-write-without-hooks

// doc-example:begin lookup-after
func lookup(ctx context.Context, tx *database.Tx) error {
	_, err := orm.FirstOrCreate(ctx, tx, models.Users.Query().Where(orm.Eq(models.Users.Email, "ada@example.test")))
	return err
}

// doc-example:end lookup-after

// doc-example:begin pagination-after
func page(ctx context.Context, db database.Executor) error {
	_, err := orm.Paginate(ctx, db, models.Orders.Query(), orm.PageRequest{Number: 1, Size: 20})
	if err != nil {
		return err
	}
	_, err = orm.CursorPaginate(ctx, db, models.Orders.Query(), orm.CursorRequest[models.Order]{Size: 20})
	return err
}

// doc-example:end pagination-after

// doc-example:begin application-after
func configuredApplication(settings application.Settings, pool foundation.Key[*database.DB]) *application.Builder {
	observers := foundation.Module{Name: "member.observers", OnRegister: func(r *foundation.Registrar) error {
		return orm.RegisterObserver(r, pool, orm.NewObserver[hooks.Member]("members"),
			func(foundation.Resolver) (func() orm.Hooks[hooks.Member], error) { return memberHooks, nil })
	}}
	return application.New(settings, application.WithTables(Users.Table, hooks.Members.Table)).Register(observers)
}

// doc-example:end application-after

// doc-example:begin seed-after
func seeder() seed.Definition {
	return seed.Definition{ID: "app.users", Run: func(ctx context.Context, tx *database.Tx) error {
		_, err := orm.Insert(ctx, tx, models.Users.Table, orm.Set(models.Users.Email, "ada@example.test"))
		return err
	}}
}

// doc-example:end seed-after

func identity(user models.User) (model.Identity, error) {
	return orm.IdentityOf(models.Users.Table, models.Users.Key()).ReferenceOf(user).Identity()
}

// doc-example:begin scalar-after
func scalar(ctx context.Context, db database.Executor) error {
	_, err := rows.Read(rows.SelectValue(models.Orders.Query(), models.Orders.TotalCents)).All(ctx, db)
	return err
}

// doc-example:end scalar-after

// doc-example:begin input-key-after
func inputKey(id model.ID[mutators.KeyRecord]) orm.Assignment[mutators.KeyRecord] {
	return orm.Set(mutators.KeyRecords.ID, mutators.FreshID{Value: id})
}

// doc-example:end input-key-after
