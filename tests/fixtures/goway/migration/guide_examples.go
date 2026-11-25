// Package migration holds the model examples of the ORM guides that use the
// hook, order and mutator fixtures; its tests check the migrated behavior.
package migration

import (
	"context"

	"foundry.test/goway/models"
	hooks "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/audit"
	"github.com/weiloon1234/Foundry-Go/audit/record"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

// doc-example:begin reads-after
func largeOrders(ctx context.Context, db database.Executor) ([]models.Order, error) {
	return orm.All(ctx, db, models.Orders.Query().Where(orm.Gt(models.Orders.TotalCents, 100)))
}

func findOrder(ctx context.Context, db database.Executor, id model.ID[models.Order]) (value.Optional[models.Order], error) {
	return orm.Find(ctx, db, models.Orders.Table, id)
}

func requireOrder(ctx context.Context, db database.Executor, id model.ID[models.Order]) (models.Order, error) {
	return orm.Require(ctx, db, models.Orders.Query().Where(orm.Eq(models.Orders.ID, id)))
}

// doc-example:end reads-after

// doc-example:begin unique-column-lock
func changeUniqueColumn(ctx context.Context, db *database.DB, id int64) error {
	return db.Transaction(ctx, func(tx *database.Tx) error {
		selected := hooks.Members.Table.Query().Where(orm.Eq(hooks.Members.Table.Key(), id))
		if _, err := orm.RequireLocked(ctx, tx, orm.Lock(selected)); err != nil {
			return err
		}
		_, err := orm.Update(ctx, tx, selected, hooks.Members.Table.Key(), id, orm.Set(hooks.Members.Name, "guest"))
		return err
	})
}

// doc-example:end unique-column-lock

// doc-example:begin writes-after
func updateMember(ctx context.Context, tx *database.Tx, id int64) (hooks.Member, error) {
	return orm.Update(ctx, tx, hooks.Members.Table.Query(), hooks.Members.Table.Key(), id, orm.Set(hooks.Members.Name, "Ada"), orm.SetNull(hooks.Members.Note))
}

// doc-example:end writes-after

// doc-example:begin insert-member
func insertMember(ctx context.Context, tx *database.Tx, id int64) (hooks.Member, error) {
	f := hooks.Members
	return orm.Insert(ctx, tx, f.Table, orm.Set(f.Key(), id), orm.Set(f.Name, "Ada"), orm.Set(f.Body, []byte{}))
}

// doc-example:end insert-member

// doc-example:begin relations-after
var OrderBuyer = orm.BelongsToOf(models.Orders.Buyer, models.Orders.BuyerID, models.Users.ID)

func ordersWithBuyers(ctx context.Context, db database.Executor) ([]models.Order, error) {
	return orm.All(ctx, db, models.Orders.Query().With(OrderBuyer))
}

func loadBuyers(ctx context.Context, db database.Executor, existing []models.Order) ([]models.Order, error) {
	return orm.LoadFrom(ctx, db, models.Orders.Query().With(OrderBuyer), existing)
}

// doc-example:end relations-after

// doc-example:begin hooks-after
func memberHooks() orm.Hooks[hooks.Member] {
	return orm.Hooks[hooks.Member]{Creating: func(_ context.Context, _ *database.Tx, draft *orm.Write[hooks.Member]) error {
		setName := func(name string) orm.Assignment[hooks.Member] { return orm.Set(hooks.Members.Name, name) }
		if !orm.WriteIsSet(draft, setName) {
			return orm.WriteSet(draft, setName("guest"))
		}
		return nil
	}}
}

// doc-example:end hooks-after

// doc-example:begin audit-registration
func registerAudit(r *foundation.Registrar, pool foundation.Key[*database.DB], history foundation.Key[*audit.Recorder]) error {
	f := hooks.Members
	policy := orm.AuditPolicyOf(f.Table, orm.AuditField(f.Note, record.Exclude))
	return audit.Register(r, pool, history,
		audit.Config{Area: "accounts", RetentionDays: 90}, orm.Auditing(f.Table, policy))
}

// doc-example:end audit-registration
