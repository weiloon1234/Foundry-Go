package reporting

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/model"
)

// The payload stores an actor reference, filters and explicit presentation.
// It contains no roles, permission claims, serialized guard or completed rows.
//
//foundry:dto
type ExportMembers struct {
	OperatorID model.ID[Operator]      `json:"operator_id"`
	Request    datatable.Request       `json:"request"`
	Options    datatable.ExportOptions `json:"options"`
}

var ExportMembersJob = jobs.Define[ExportMembers]("reports.export-members", 1, jobs.DefaultPolicy("exports"))

// Destination receives a stable job-owned identity for its idempotent storage.
// The handler owns the artifact; the destination must finish reading before
// returning, and may not retain it for a detached goroutine.
type Destination interface {
	Store(context.Context, jobs.ID[ExportMembers], *datatable.Artifact) error
}

func ExportDeclaration(manager *datatable.Manager, provider OperatorProvider, destination Destination) (jobs.Declaration, error) {
	if err := provider.Validate(); err != nil {
		return jobs.Declaration{}, err
	}
	if destination == nil {
		return jobs.Declaration{}, fault.New(fault.Invalid, "export destination is required")
	}
	handler, err := datatable.ExportHandler(Members, manager, func(ctx context.Context, payload ExportMembers) (Authority, datatable.Request, datatable.ExportOptions, error) {
		actor, err := provider.Resolve(ctx, (Operator{ID: payload.OperatorID}).FoundryReference())
		if err != nil {
			return Authority{}, datatable.Request{}, datatable.ExportOptions{}, err
		}
		return Authority{actor: actor, resolved: true}, payload.Request, payload.Options, nil
	}, func(ctx context.Context, _ ExportMembers, artifact *datatable.Artifact) error {
		id, present := ExportMembersJob.CurrentID(ctx)
		if !present {
			return fault.New(fault.Invalid, "export delivery requires a live job attempt")
		}
		return destination.Store(ctx, id, artifact)
	})
	if err != nil {
		return jobs.Declaration{}, err
	}
	return ExportMembersJob.Declare(handler)
}
