package idempotenthttp

import (
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:model table=idem_workspaces primary=ID
type Workspace struct {
	ID      int64
	Tenant  string
	Enabled bool
}

//foundry:model table=idem_orders
type Order struct {
	ID          model.ID[Order]
	WorkspaceID int64
	Caller      int64
	Name        string
}

//foundry:path pattern=/workspaces/{workspace}/orders
type Path struct{ Workspace int64 }

//foundry:dto
type Submission struct {
	Name    string                                 `json:"name"`
	Memo    value.Optional[value.Nullable[string]] `json:"memo,omitzero"`
	Failure string                                 `json:"failure,omitempty"`
}

//foundry:dto
type Receipt struct {
	ID    model.ID[Order]                        `json:"id"`
	Name  string                                 `json:"name"`
	Memo  value.Optional[value.Nullable[string]] `json:"memo,omitzero"`
	Score float64                                `json:"score"`
}
