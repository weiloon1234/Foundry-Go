package bootstrap

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/jobs"
)

// doc-example:begin supporting-services-1
// ReportID identifies the report requested by a queued task.
type ReportID string

// ReportPayload carries the report identifier across the job boundary.
type ReportPayload struct {
	// ReportID selects the report to build.
	ReportID ReportID `json:"report_id"`
}

// ReportJob shares its identity between registration and dispatch.
var ReportJob = jobs.Define[ReportPayload]("reports.build", 1, jobs.DefaultPolicy("reports"))

func queueReport(ctx context.Context, services application.Services, id ReportID) error {
	connection, err := services.JobConnection()
	if err != nil {
		return err
	}
	work, err := ReportJob.On(connection)
	if err != nil {
		return err
	}
	_, err = work.Dispatch(ctx, ReportPayload{ReportID: id}, jobs.Options[ReportPayload]{})
	return err
}

// doc-example:end supporting-services-1
