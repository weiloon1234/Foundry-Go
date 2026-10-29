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

// migration:roles-after
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

// migration:end

// migration:reads-after
func read(ctx context.Context, db database.Executor, id model.ID[models.Order]) error {
	_, err := orm.All(ctx, db, models.Orders.Query().Where(orm.NumberGt(models.Orders.TotalCents, 100)))
	if err != nil {
		return err
	}
	_, err = orm.Find(ctx, db, models.Orders.Table, id)
	if err != nil {
		return err
	}
	_, err = orm.Require(ctx, db, models.Orders.Query().Where(orm.KeyEq(models.Orders.ID, id)))
	return err
}

// migration:end

// migration:writes-after
func write(ctx context.Context, tx *database.Tx, id int64) error {
	_, err := orm.Update(ctx, tx, hooks.Members.Table.Query(), hooks.Members.Table.Key(), id, orm.InputTextSet(hooks.Members.Name, "Ada"), orm.NullableInputTextSetNull(hooks.Members.Note))
	return err
}

// migration:end

// migration:insert-after
func insert(ctx context.Context, tx *database.Tx) error {
	_, err := orm.Insert(ctx, tx, models.Users.Table, orm.TextSet(models.Users.Email, "ada@example.test"))
	return err
}

// migration:end

// migration:force-after
func forceDelete(ctx context.Context, tx *database.Tx, id int64) error {
	_, err := orm.ForceDelete(ctx, tx, Users.Query().WithTrashed(), Users.Soft, Users.Key(), id)
	return err
}

// migration:end

// migration:relations-after
var OrderBuyer = orm.BelongsToOf(models.Orders.Buyer, orm.ColumnExpr(models.Orders.BuyerID), orm.KeyExpr(models.Users.ID))

func relations(ctx context.Context, db database.Executor, existing []models.Order) error {
	_, err := orm.All(ctx, db, models.Orders.Query().With(OrderBuyer))
	if err != nil {
		return err
	}
	_, err = orm.LoadFrom(ctx, db, models.Orders.Query().With(OrderBuyer), existing)
	return err
}

// migration:end

// migration:hooks-after
func memberHooks() orm.Hooks[hooks.Member] {
	return orm.Hooks[hooks.Member]{Creating: func(_ context.Context, _ *database.Tx, draft *orm.Write[hooks.Member]) error {
		if !orm.WriteIsSet(draft, func() func(arg0 string) orm.Assignment[hooks.Member] {
			receiver := hooks.Members.Name
			return func(arg0 string) orm.Assignment[hooks.Member] { return orm.InputTextSet(receiver, arg0) }
		}()) {
			return orm.WriteSet(draft, orm.InputTextSet(hooks.Members.Name, "guest"))
		}
		return nil
	}}
}

// migration:end

// migration:unset-after
func unset(draft *orm.Write[hooks.Member]) error {
	return orm.WriteUnset(draft, func() func(arg0 coremodels.LabelInput) orm.Assignment[hooks.Member] {
		receiver := hooks.Members.Note
		return func(arg0 coremodels.LabelInput) orm.Assignment[hooks.Member] {
			return orm.NullableInputTextSet(receiver, arg0)
		}
	}())
}

// migration:end

// migration:changes-after
func changes(changes orm.Changes[hooks.Member]) (bool, error) {
	field, err := orm.ChangeOf(changes, orm.InputTextExpr(hooks.Members.Name))
	return field.Changed(), err
}

// migration:end

// migration:observers-after
func observer() (lifecycle.Declaration, error) {
	return orm.NewObserver[hooks.Member]("members").Declare(memberHooks)
}

// migration:end

// migration:retrieved-after
func retrieval() orm.RetrievalHooks[reads.Member] {
	return orm.RetrievalHooks[reads.Member]{Retrieved: func(context.Context, database.Executor, reads.Member) error { return nil }}
}

// migration:end

// migration:scopes-after
func scope(t reads.MemberTable) orm.GlobalScope[reads.Member] {
	return orm.Scope(t.Table, "tenant", func(ctx context.Context) (orm.Predicate[reads.Member], error) {
		id, err := tenantID(ctx)
		return orm.NumberEq(t.TenantID, id), err
	})
}

// migration:end

type tenantKey struct{}

func tenantID(ctx context.Context) (int64, error) {
	id, ok := ctx.Value(tenantKey{}).(int64)
	if !ok {
		return 0, errors.New("tenant required")
	}
	return id, nil
}

// migration:bypass-after
func bypass() orm.Query[reads.Member] {
	return orm.WithoutGlobalScope(reads.Members.Query(), reads.MemberTenant(reads.Members))
}

// migration:end

// migration:set-writes-after
func setWrite(ctx context.Context, tx *database.Tx) error {
	_, err := orm.UpdateAll(ctx, tx, orm.WithoutModelHooks(hooks.Members.Query()), orm.InputTextSet(hooks.Members.Name, "guest"))
	return err
}

// migration:end

// migration:lookup-after
func lookup(ctx context.Context, tx *database.Tx) error {
	_, err := orm.FirstOrCreate(ctx, tx, models.Users.Query().Where(orm.TextEq(models.Users.Email, "ada@example.test")))
	return err
}

// migration:end

// migration:pagination-after
func page(ctx context.Context, db database.Executor) error {
	_, err := orm.Paginate(ctx, db, models.Orders.Query(), orm.PageRequest{Number: 1, Size: 20})
	if err != nil {
		return err
	}
	_, err = orm.CursorPaginate(ctx, db, models.Orders.Query(), orm.CursorRequest[models.Order]{Size: 20})
	return err
}

// migration:end

// migration:application-after
func configuredApplication(settings application.Settings, pool foundation.Key[*database.DB]) *application.Builder {
	observers := foundation.Module{Name: "member.observers", OnRegister: func(r *foundation.Registrar) error {
		return orm.RegisterObserver(r, pool, orm.NewObserver[hooks.Member]("members"),
			func(foundation.Resolver) (func() orm.Hooks[hooks.Member], error) { return memberHooks, nil })
	}}
	return application.New(settings, application.WithTables(Users.Table, hooks.Members.Table)).Register(observers)
}

// migration:end

// migration:seed-after
func seeder() seed.Definition {
	return seed.Definition{ID: "app.users", Run: func(ctx context.Context, tx *database.Tx) error {
		_, err := orm.Insert(ctx, tx, models.Users.Table, orm.TextSet(models.Users.Email, "ada@example.test"))
		return err
	}}
}

// migration:end

// migration:identity-after
func identity(user models.User) (model.Identity, error) {
	return orm.IdentityOf(models.Users.Table, models.Users.Key()).ReferenceOf(user).Identity()
}

// migration:end

// migration:scalar-after
func scalar(ctx context.Context, db database.Executor) error {
	_, err := rows.Read(rows.SelectValue(models.Orders.Query(), orm.NumberExpr(models.Orders.TotalCents))).All(ctx, db)
	return err
}

// migration:end

// migration:inputs-after
func input(input coremodels.EmailInput) orm.Assignment[mutators.Member] {
	return orm.InputTextSet(mutators.Members.Email, input)
}

// migration:end

// migration:accessors-after
func accessor(member mutators.Member) (coremodels.EmailLink, error) {
	return member.AccessEmail()
}

// migration:end

// migration:input-key-after
func inputKey(id model.ID[mutators.KeyRecord]) orm.Assignment[mutators.KeyRecord] {
	return orm.InputKeySet(mutators.KeyRecords.ID, mutators.FreshID{Value: id})
}

// migration:end
