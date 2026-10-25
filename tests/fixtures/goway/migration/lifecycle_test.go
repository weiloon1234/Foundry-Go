package migration_test

import (
	"context"
	"strings"
	"time"

	"foundry.test/goway/models"
	hooks "foundry.test/goway/models/hooks"
	reads "foundry.test/goway/models/reads"
	"github.com/weiloon1234/Foundry-Go/audit"
	"github.com/weiloon1234/Foundry-Go/audit/record"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/database/prune"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/value"
)

// guide:write-options
func writeOptions() []orm.Option {
	return []orm.Option{orm.WithHooks(memberHooks)}
}

// guide:end

// guide:register-observer
func registerMemberObserver(r *foundation.Registrar, pool foundation.Key[*database.DB]) error {
	return orm.RegisterObserver(r, pool, orm.NewObserver[hooks.Member]("members.defaults"),
		func(foundation.Resolver) (func() orm.Hooks[hooks.Member], error) {
			return memberHooks, nil
		})
}

// guide:end

// guide:read-options
func readOptions() []orm.Option {
	return []orm.Option{orm.WithRetrievalHooks(retrieval)}
}

func registerReadObserver(r *foundation.Registrar, pool foundation.Key[*database.DB]) error {
	return orm.RegisterRetrievalObserver(r, pool, orm.NewRetrievalObserver[reads.Member]("members.read"),
		func(foundation.Resolver) (func() orm.RetrievalHooks[reads.Member], error) {
			return retrieval, nil
		})
}

// guide:end

// guide:scope-options
func scopeOptions() []orm.Option {
	return []orm.Option{orm.WithGlobalScopes(func(t reads.MemberTable) []orm.GlobalScope[reads.Member] {
		return []orm.GlobalScope[reads.Member]{scope(t)}
	})}
}

// guide:end

// guide:stored-comparison
func compareStored(before, after hooks.Member) (orm.Changes[hooks.Member], error) {
	return orm.Compare(hooks.Members.Table, value.Set(before), value.Set(after),
		hooks.Members.Name.Set(after.Name))
}

// guide:end

// guide:each-writes
func updateMembers(ctx context.Context, tx *database.Tx) ([]hooks.Member, error) {
	f := hooks.Members
	return orm.UpdateEach(ctx, tx, f.Query().Where(f.Name.Eq("guest")), 100,
		func(_ context.Context, _ *database.Tx, current hooks.Member) ([]orm.Assignment[hooks.Member], error) {
			return []orm.Assignment[hooks.Member]{f.Name.Set("member-" + current.Name)}, nil
		})
}

func createMembers(ctx context.Context, tx *database.Tx) ([]hooks.Member, error) {
	f := hooks.Members
	return orm.CreateEach(ctx, tx, f.Query(), [][]orm.Assignment[hooks.Member]{
		{f.Key().Set(1), f.Name.Set("first"), f.Body.Set([]byte{})},
		{f.Key().Set(2), f.Name.Set("second"), f.Body.Set([]byte{})},
	})
}

// guide:end

// guide:each-soft-writes
func restoreMembers(ctx context.Context, tx *database.Tx) ([]hooks.Member, error) {
	f := hooks.Members
	return orm.RestoreEach(ctx, tx, f.Query().Where(f.Name.Eq("guest")), f.Soft, 100)
}

func removeMembers(ctx context.Context, tx *database.Tx) ([]hooks.Member, error) {
	f := hooks.Members
	return orm.ForceDeleteEach(ctx, tx, f.Query().OnlyTrashed(), f.Soft, 100)
}

// guide:end

// guide:adjustments
func addCredit(ctx context.Context, tx *database.Tx) (int64, error) {
	f := models.Orders
	return f.Query().Where(f.TotalCents.Lt(1000)).UpdateAll(ctx, tx,
		f.TotalCents.Increment(100, orm.AtMost[int64](1000)))
}

// guide:end

// guide:lookup-branches
func createOrFindUser(ctx context.Context, tx *database.Tx) (models.User, error) {
	f := models.Users
	return orm.CreateOrFirst(ctx, tx, f.Query().Where(f.Email.Eq("ada@example.test")), f.Age.Set(30), f.Status.Set(models.StatusActive), f.Level.Set(models.LevelBasic))
}

func updateOrCreateUser(ctx context.Context, tx *database.Tx) (models.User, error) {
	f := models.Users
	return orm.UpdateOrCreate(ctx, tx, f.Query().Where(f.Email.Eq("ada@example.test")),
		[]orm.Assignment[models.User]{f.Age.Set(30), f.Status.Set(models.StatusActive), f.Level.Set(models.LevelBasic)},
		func(context.Context, *database.Tx, models.User) ([]orm.Assignment[models.User], error) {
			return []orm.Assignment[models.User]{f.Age.Set(31)}, nil
		})
}

// guide:end

// guide:source-insert
type ArchivedMember struct {
	ID                   int64
	Name                 string
	Body                 []byte
	CreatedAt, UpdatedAt time.Time
}

func (ArchivedMember) MutateName(name string) (string, error) {
	return strings.TrimSpace(name), nil
}

type ArchiveTable struct {
	orm.Table[ArchivedMember, int64]
	ID        orm.Primary[ArchivedMember, int64]
	Name      orm.InputText[ArchivedMember, string, string]
	Body      orm.Column[ArchivedMember, []byte]
	CreatedAt orm.CreatedAt[ArchivedMember, time.Time]
	UpdatedAt orm.UpdatedAt[ArchivedMember, time.Time]
}

var Archives = orm.Define("member_archives", func(b orm.Builder[ArchivedMember], m *ArchivedMember) ArchiveTable {
	table := orm.TableOf(b, &m.ID)
	return ArchiveTable{table, orm.PrimaryOf(b, table.Key()),
		orm.InputTextOf[ArchivedMember, string](b, &m.Name, nil), orm.BytesOf(b, &m.Body),
		orm.CreatedAtOf(b, &m.CreatedAt), orm.UpdatedAtOf(b, &m.UpdatedAt)}
})

func copyMembers(ctx context.Context, tx *database.Tx) (int64, error) {
	source, destination := hooks.Members, Archives
	return rows.InsertFrom(rows.From(source.Query()), destination.Query(),
		rows.MapInsert(destination.ID, expr.Value(source.Key())),
		rows.MapInsert(destination.Body, expr.Value(source.Body)),
		rows.MapInsert(destination.CreatedAt, expr.Value(source.CreatedAt))).
		Values(destination.Name.Set("copy")).Exec(ctx, tx)
}

// guide:end

// guide:source-update
func updateMemberBodies(ctx context.Context, tx *database.Tx) (int64, error) {
	f := hooks.Members
	destination := f.Query()
	return rows.UpdateFrom(destination, rows.From(f.Query())).
		Match(rows.MatchSource(f.Key(), expr.Value(f.Key()))).
		Select(rows.MapUpdate(f.Body, expr.Value(f.Body))).
		Values(f.Name.Set("updated")).Exec(ctx, tx)
}

// guide:end

// guide:source-delete
func deleteMatchingMembers(ctx context.Context, tx *database.Tx) (int64, error) {
	f := hooks.Members
	destination := f.Query().OnlyTrashed()
	return rows.ForceDeleteUsing(destination, f.Soft, rows.From(f.Query().OnlyTrashed())).
		Match(rows.MatchSource(f.Key(), expr.Value(f.Key()))).Exec(ctx, tx)
}

// guide:end

// guide:pruning
func pruneMembers(ctx context.Context, db database.Transactor, cutoff time.Time) (prune.Result, error) {
	f := hooks.Members
	registry, err := prune.New(prune.ORMSoftModel("app.old_members",
		f.Query().OnlyTrashed().Where(f.UpdatedAt.Lt(cutoff)), f.Soft, prune.Lifecycle))
	if err != nil {
		return prune.Result{}, err
	}
	return registry.Run(ctx, db, prune.DefaultOptions())
}

// guide:end

// guide:explain
func inspectOrders(ctx context.Context, db database.Executor) (orm.Plan, error) {
	return orm.Explain(ctx, db, models.Orders.Query().Where(models.Orders.TotalCents.Gt(100)))
}

func analyzeOrders(ctx context.Context, tx *database.Tx) (orm.Plan, error) {
	return orm.ExplainAnalyzeLocked(ctx, tx, models.Orders.Query().ForUpdate())
}

// guide:end

// guide:audit-registration
func registerAudit(r *foundation.Registrar, pool foundation.Key[*database.DB], history foundation.Key[*audit.Recorder]) error {
	f := hooks.Members
	policy := orm.AuditPolicyOf(f.Table, orm.AuditField(f.Note, record.Exclude))
	return audit.Register(r, pool, history,
		audit.Config{Area: "accounts", RetentionDays: 90}, orm.Auditing(f.Table, policy))
}

// guide:end

// guide:audit-timeline
func memberHistory(ctx context.Context, db database.Executor, recorder *audit.Recorder, member hooks.Member) error {
	identity := orm.IdentityOf(hooks.Members.Table, hooks.Members.Key())
	subject := identity.ReferenceOf(member)
	page, err := audit.ModelTimeline(ctx, db, recorder, subject, audit.TimelineRequest{Size: 20})
	if err != nil {
		return err
	}
	if next, more := page.Next.Get(); more {
		_, err = audit.ModelTimeline(ctx, db, recorder, subject,
			audit.TimelineRequest{Size: 20, Before: value.Set(next)})
	}
	return err
}

// guide:end

// guide:audit-field
func historicalName(history record.Model[hooks.Member, int64]) (value.Optional[record.FieldChange[string]], error) {
	return orm.AuditRead(history, hooks.Members.Name)
}

// guide:end

// guide:audit-find
func findHistory(ctx context.Context, db database.Executor, recorder *audit.Recorder, id audit.ModelID[hooks.Member]) error {
	identity := orm.IdentityOf(hooks.Members.Table, hooks.Members.Key())
	_, err := audit.FindModel(ctx, db, recorder, identity.ReferenceOf(hooks.Member{}), id)
	return err
}

// guide:end

// guide:audit-action
type Approval struct {
	Reason string `json:"reason"`
}

var Approved = audit.Define[Approval]("member.approved", 1)

func approve(ctx context.Context, tx *database.Tx, recorder *audit.Recorder, member hooks.Member) error {
	identity := orm.IdentityOf(hooks.Members.Table, hooks.Members.Key())
	_, err := audit.RecordFor(ctx, tx, recorder, Approved,
		identity.ReferenceOf(member), Approval{Reason: "review completed"})
	return err
}

// guide:end

// guide:insert-member
func insertMember(ctx context.Context, tx *database.Tx, id int64) (hooks.Member, error) {
	f := hooks.Members
	return f.Insert(ctx, tx, f.Key().Set(id), f.Name.Set("Ada"), f.Body.Set([]byte{}))
}

// guide:end

// guide:upsert-user
func upsertUser(ctx context.Context, tx *database.Tx) (value.Optional[models.User], error) {
	f := models.Users
	policy := orm.OnConflict(orm.ConflictKey(f.Email)).DoUpdate(orm.ConflictIncoming(f.Age))
	return orm.Upsert(ctx, tx, f.Query(), policy,
		f.Email.Set("ada@example.test"), f.Age.Set(30),
		f.Status.Set(models.StatusActive), f.Level.Set(models.LevelBasic))
}

// guide:end

// guide:first-or-create-user
func firstOrCreateUser(ctx context.Context, tx *database.Tx) (models.User, error) {
	f := models.Users
	return orm.FirstOrCreate(ctx, tx, f.Query().Where(f.Email.Eq("ada@example.test")),
		f.Age.Set(30), f.Status.Set(models.StatusActive), f.Level.Set(models.LevelBasic))
}

// guide:end

// guide:audit-page
func memberHistoryPage(ctx context.Context, db database.Executor, recorder *audit.Recorder, member hooks.Member) error {
	identity := orm.IdentityOf(hooks.Members.Table, hooks.Members.Key())
	_, err := audit.ModelHistory(ctx, db, recorder, identity.ReferenceOf(member),
		orm.PageRequest{Number: 1, Size: 20})
	return err
}

// guide:end
