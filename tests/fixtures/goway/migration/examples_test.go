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
	old "foundry.test/goway/oldmodels"
	oldhooks "foundry.test/goway/oldmodels/hooks"
	oldmutators "foundry.test/goway/oldmodels/mutators"
	oldreads "foundry.test/goway/oldmodels/reads"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/database/seed"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

// migration:roles-before
func legacyRoles() oldhooks.MemberFieldSet {
	return oldhooks.MemberFields()
}

// migration:end

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

// migration:reads-before
func legacyRead(ctx context.Context, db database.Executor, id model.ID[old.Order]) error {
	_, err := old.QueryOrders().Where(old.OrderFields().TotalCents.Gt(100)).All(ctx, db)
	if err != nil {
		return err
	}
	_, err = old.QueryOrders().Find(ctx, db, id)
	if err != nil {
		return err
	}
	_, err = old.QueryOrders().Where(old.OrderFields().ID.Eq(id)).RequireFirst(ctx, db)
	return err
}

// migration:end
// migration:reads-after
func read(ctx context.Context, db database.Executor, id model.ID[models.Order]) error {
	_, err := models.Orders.Query().Where(models.Orders.TotalCents.Gt(100)).All(ctx, db)
	if err != nil {
		return err
	}
	_, err = models.Orders.Find(ctx, db, id)
	if err != nil {
		return err
	}
	_, err = models.Orders.Query().Where(models.Orders.ID.Eq(id)).Require(ctx, db)
	return err
}

// migration:end

// migration:writes-before
func legacyWrite(ctx context.Context, tx *database.Tx, id int64) error {
	draft := oldhooks.MemberDraft{}.SetName("Ada").ClearNote()
	_, err := oldhooks.QueryHookMembers().Update(ctx, tx, id, draft)
	return err
}

// migration:end
// migration:writes-after
func write(ctx context.Context, tx *database.Tx, id int64) error {
	_, err := hooks.Members.Update(ctx, tx, id,
		hooks.Members.Name.Set("Ada"), hooks.Members.Note.SetNull())
	return err
}

// migration:end

// migration:insert-before
func legacyInsert(ctx context.Context, tx *database.Tx) error {
	_, err := old.QueryUsers().Create(ctx, tx, old.UserDraft{}.SetEmail("ada@example.test"))
	return err
}

// migration:end
// migration:insert-after
func insert(ctx context.Context, tx *database.Tx) error {
	_, err := models.Users.Insert(ctx, tx, models.Users.Email.Set("ada@example.test"))
	return err
}

// migration:end

// migration:force-before
func legacyForceDelete(ctx context.Context, tx *database.Tx, id int64) error {
	_, err := oldhooks.QueryHookMembers().WithTrashed().ForceDelete(ctx, tx, id)
	return err
}

// migration:end
// migration:force-after
func forceDelete(ctx context.Context, tx *database.Tx, id int64) error {
	_, err := orm.ForceDelete(ctx, tx, Users.Query().WithTrashed(), Users.Soft, Users.Key(), id)
	return err
}

// migration:end

// migration:relations-before
func legacyRelations(ctx context.Context, db database.Executor) error {
	_, err := old.QueryOrders().With(old.OrderRelations().Buyer).All(ctx, db)
	return err
}

// migration:end
// migration:relations-after
var OrderBuyer = orm.BelongsToOf(models.Orders.Buyer, models.Orders.BuyerID, models.Users.ID)

func relations(ctx context.Context, db database.Executor, existing []models.Order) error {
	_, err := models.Orders.Query().With(OrderBuyer).All(ctx, db)
	if err != nil {
		return err
	}
	_, err = orm.Load(ctx, db, existing, OrderBuyer)
	return err
}

// migration:end

// migration:hooks-before
func legacyHooks() oldhooks.MemberHooks {
	return oldhooks.MemberHooks{Creating: func(_ context.Context, _ *database.Tx, draft *oldhooks.MemberDraft) error {
		if !draft.Name().IsSet() {
			*draft = draft.SetName("guest")
		}
		return nil
	}}
}

// migration:end
// migration:hooks-after
func memberHooks() orm.Hooks[hooks.Member] {
	return orm.Hooks[hooks.Member]{Creating: func(_ context.Context, _ *database.Tx, draft *orm.Write[hooks.Member]) error {
		if !orm.WriteIsSet(draft, hooks.Members.Name.Set) {
			return orm.WriteSet(draft, hooks.Members.Name.Set("guest"))
		}
		return nil
	}}
}

// migration:end

// migration:unset-before
func legacyUnset(draft *oldhooks.MemberDraft) { *draft = draft.UnsetNote() }

// migration:end
// migration:unset-after
func unset(draft *orm.Write[hooks.Member]) error {
	return orm.WriteUnset(draft, hooks.Members.Note.Set)
}

// migration:end

// migration:changes-before
func legacyChanges(changes oldhooks.MemberChanges) bool { return changes.Fields().Name.Changed() }

// migration:end
// migration:changes-after
func changes(changes orm.Changes[hooks.Member]) (bool, error) {
	field, err := orm.ChangeOf(changes, hooks.Members.Name)
	return field.Changed(), err
}

// migration:end

// migration:observers-before
func legacyObserver() (lifecycle.Declaration, error) {
	return oldhooks.NewMemberObserver("members").Declare(legacyHooks)
}

// migration:end
// migration:observers-after
func observer() (lifecycle.Declaration, error) {
	return orm.NewObserver[hooks.Member]("members").Declare(memberHooks)
}

// migration:end

// migration:retrieved-before
func legacyRetrieval() oldreads.MemberRetrievalHooks {
	return oldreads.MemberRetrievalHooks{Retrieved: func(context.Context, database.Executor, oldreads.Member) error { return nil }}
}

// migration:end
// migration:retrieved-after
func retrieval() orm.RetrievalHooks[reads.Member] {
	return orm.RetrievalHooks[reads.Member]{Retrieved: func(context.Context, database.Executor, reads.Member) error { return nil }}
}

// migration:end

// migration:scopes-before
func legacyScope() query.GlobalScope[oldreads.Member] {
	return query.NewContextScope[oldreads.Member]("tenant", func(ctx context.Context) (query.Predicate[oldreads.Member], error) {
		id, err := tenantID(ctx)
		return oldreads.MemberFields().TenantID.Eq(id), err
	})
}

// migration:end
// migration:scopes-after
func scope(t reads.MemberTable) orm.GlobalScope[reads.Member] {
	return orm.Scope(t.Table, "tenant", func(ctx context.Context) (orm.Predicate[reads.Member], error) {
		id, err := tenantID(ctx)
		return t.TenantID.Eq(id), err
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

// migration:bypass-before
func legacyBypass() oldreads.MemberQuery {
	return oldreads.QueryReadMembers().WithoutGlobalScope(oldreads.MemberTenant())
}

// migration:end
// migration:bypass-after
func bypass() orm.Query[reads.Member] {
	return orm.WithoutGlobalScope(reads.Members.Query(), reads.MemberTenant(reads.Members))
}

// migration:end

// migration:set-writes-before
func legacySetWrite(ctx context.Context, tx *database.Tx) error {
	_, err := oldhooks.QueryHookMembers().WithoutModelHooks().UpdateAll(ctx, tx, oldhooks.MemberDraft{}.SetName("guest"))
	return err
}

// migration:end
// migration:set-writes-after
func setWrite(ctx context.Context, tx *database.Tx) error {
	_, err := orm.WithoutModelHooks(hooks.Members.Query()).UpdateAll(ctx, tx, hooks.Members.Name.Set("guest"))
	return err
}

// migration:end

// migration:lookup-before
func legacyLookup(ctx context.Context, tx *database.Tx) error {
	_, err := old.QueryUsers().Where(old.UserFields().Email.Eq("ada@example.test")).FirstOrCreate(ctx, tx, old.UserDraft{})
	return err
}

// migration:end
// migration:lookup-after
func lookup(ctx context.Context, tx *database.Tx) error {
	_, err := orm.FirstOrCreate(ctx, tx, models.Users.Query().Where(models.Users.Email.Eq("ada@example.test")))
	return err
}

// migration:end

// migration:pagination-before
func legacyPage(ctx context.Context, db database.Executor) error {
	_, err := old.QueryOrders().Paginate(ctx, db, query.PageRequest{Number: 1, Size: 20})
	if err != nil {
		return err
	}
	_, err = old.QueryOrders().CursorPaginate(ctx, db, query.CursorRequest[old.Order]{Size: 20})
	return err
}

// migration:end
// migration:pagination-after
func page(ctx context.Context, db database.Executor) error {
	_, err := models.Orders.Query().Paginate(ctx, db, orm.PageRequest{Number: 1, Size: 20})
	if err != nil {
		return err
	}
	_, err = orm.CursorPaginate(ctx, db, models.Orders.Query(), orm.CursorRequest[models.Order]{Size: 20})
	return err
}

// migration:end

// migration:application-before
func legacyApplication(settings application.Settings) *application.Builder {
	return application.New(settings)
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

// migration:seed-before
func legacySeeder() seed.Definition {
	return seed.Definition{ID: "app.users", Run: func(ctx context.Context, tx *database.Tx) error {
		_, err := old.QueryUsers().Create(ctx, tx, old.UserDraft{}.SetEmail("ada@example.test"))
		return err
	}}
}

// migration:end
// migration:seed-after
func seeder() seed.Definition {
	return seed.Definition{ID: "app.users", Run: func(ctx context.Context, tx *database.Tx) error {
		_, err := models.Users.Insert(ctx, tx, models.Users.Email.Set("ada@example.test"))
		return err
	}}
}

// migration:end

// migration:identity-before
func legacyIdentity(user old.User) (model.Identity, error) { return user.FoundryIdentity() }

// migration:end
// migration:identity-after
func identity(user models.User) (model.Identity, error) {
	return orm.IdentityOf(models.Users.Table, models.Users.Key()).ReferenceOf(user).Identity()
}

// migration:end

// migration:scalar-before
func legacyScalar(ctx context.Context, db database.Executor) error {
	_, err := query.SelectValue(old.QueryOrders(), query.Value(old.OrderFields().TotalCents)).All(ctx, db)
	return err
}

// migration:end
// migration:scalar-after
func scalar(ctx context.Context, db database.Executor) error {
	_, err := rows.SelectValue(models.Orders.Query(), models.Orders.TotalCents).All(ctx, db)
	return err
}

// migration:end

// migration:inputs-before
func legacyInput(input coremodels.EmailInput) oldmutators.MemberDraft {
	return oldmutators.MemberDraft{}.SetEmail(input)
}

// migration:end
// migration:inputs-after
func input(input coremodels.EmailInput) orm.Assignment[mutators.Member] {
	return mutators.Members.Email.Set(input)
}

// migration:end

// migration:accessors-before
func legacyAccessor(member oldmutators.Member) (coremodels.EmailLink, error) {
	return member.AccessEmail()
}

// migration:end
// migration:accessors-after
func accessor(member mutators.Member) (coremodels.EmailLink, error) {
	return member.AccessEmail()
}

// migration:end

// migration:input-key-before
func legacyInputKey(id model.ID[oldmutators.KeyRecord]) oldmutators.KeyRecordDraft {
	return oldmutators.KeyRecordDraft{}.SetID(oldmutators.FreshID{Value: id})
}

// migration:end
// migration:input-key-after
func inputKey(id model.ID[mutators.KeyRecord]) orm.Assignment[mutators.KeyRecord] {
	return mutators.KeyRecords.ID.Set(mutators.FreshID{Value: id})
}

// migration:end
