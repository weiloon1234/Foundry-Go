package idempotenthttp

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

// WorkspaceTable declares the stored workspace fields. Example: Workspaces.Query().
type WorkspaceTable struct {
	// Table owns the primary key. Example: Workspaces.Key().Eq(id).
	orm.Table[Workspace, int64]
	// Tenant binds the Tenant field. Example: Workspaces.Tenant.Set("tenant").
	Tenant orm.Text[Workspace, string]
	// Enabled binds the Enabled field. Example: Workspaces.Enabled.Set(true).
	Enabled orm.Column[Workspace, bool]
}

// Workspaces preserves the persisted table and codecs. Example: Workspaces.Query().
var Workspaces = orm.Define("idem_workspaces", func(b orm.Builder[Workspace], m *Workspace) WorkspaceTable {
	return WorkspaceTable{
		Table:   orm.TableOf(b, &m.ID),
		Tenant:  orm.TextOf(b, &m.Tenant),
		Enabled: orm.ColumnOf(b, &m.Enabled),
	}
})

// OrderTable declares the stored order fields. Example: Orders.Query().
type OrderTable struct {
	// Table owns the primary key. Example: Orders.Key().Eq(id).
	orm.Table[Order, model.ID[Order]]
	// WorkspaceID binds the WorkspaceID field. Example: Orders.WorkspaceID.Set(1).
	WorkspaceID orm.Number[Order, int64]
	// Caller binds the Caller field. Example: Orders.Caller.Set(1).
	Caller orm.Number[Order, int64]
	// Name binds the Name field. Example: Orders.Name.Set("order").
	Name orm.Text[Order, string]
}

// Orders preserves the persisted table and codecs. Example: Orders.Query().
var Orders = orm.Define("idem_orders", func(b orm.Builder[Order], m *Order) OrderTable {
	return OrderTable{
		Table:       orm.TableOf(b, &m.ID),
		WorkspaceID: orm.NumberOf(b, &m.WorkspaceID),
		Caller:      orm.NumberOf(b, &m.Caller),
		Name:        orm.TextOf(b, &m.Name),
	}
})

// Tables lists declarations for startup validation. Example: orm.Check(Tables()...).
func Tables() []orm.CheckedTable { return []orm.CheckedTable{Workspaces, Orders} }
