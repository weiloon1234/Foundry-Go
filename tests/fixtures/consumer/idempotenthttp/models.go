package idempotenthttp

import (
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:model table=idem_workspaces var=Workspaces key=ID identity=false
type Workspace struct {
	// Table owns the primary key. Example: orm.Eq(Workspaces.Key(), id).
	ID int64
	// Tenant binds the Tenant field. Example: orm.Set(Workspaces.Tenant, "tenant").
	Tenant string
	// Enabled binds the Enabled field. Example: orm.Set(Workspaces.Enabled, true).
	Enabled bool `foundry:"role=column"`
}

//foundry:model table=idem_orders var=Orders key=ID identity=false
type Order struct {
	// Table owns the primary key. Example: orm.Eq(Orders.Key(), id).
	ID model.ID[Order]
	// WorkspaceID binds the WorkspaceID field. Example: orm.Set(Orders.WorkspaceID, 1).
	WorkspaceID int64
	// Caller binds the Caller field. Example: orm.Set(Orders.Caller, 1).
	Caller int64
	// Name binds the Name field. Example: orm.Set(Orders.Name, "order").
	Name string
}

//foundry:path pattern=/workspaces/{workspace}/orders
type Path struct{ Workspace int64 }

//foundry:dto http=both
type Submission struct {
	Name    string                                 `json:"name"`
	Memo    value.Optional[value.Nullable[string]] `json:"memo,omitzero"`
	Failure string                                 `json:"failure,omitempty"`
}

//foundry:dto http=response
type Receipt struct {
	ID    model.ID[Order]                        `json:"id"`
	Name  string                                 `json:"name"`
	Memo  value.Optional[value.Nullable[string]] `json:"memo,omitzero"`
	Score float64                                `json:"score"`
}
