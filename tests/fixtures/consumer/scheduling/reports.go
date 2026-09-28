// Package scheduling demonstrates domain-only schedules using public APIs.
package scheduling

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/schedule"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

const DailyReport schedule.ID = "reports.daily"

var SchedulerKey = foundation.NewKey[*schedule.Scheduler]("reports.scheduler")

type Reports interface {
	BuildDaily(context.Context, time.Time) error
}

func ReportSchedule(reports Reports) (schedule.Declaration, error) {
	zone, err := temporal.ParseTimeZone("Asia/Kuala_Lumpur")
	if err != nil {
		return schedule.Declaration{}, err
	}
	return schedule.DailyAt(DailyReport, "03:00", zone, func(ctx context.Context, invocation schedule.Invocation) error {
		return reports.BuildDaily(ctx, invocation.IntendedAt)
	})
}
func NextReport(declaration schedule.Declaration, after time.Time) (time.Time, error) {
	spec := declaration.Spec()
	return spec.Next(after)
}
func Module(reports Reports, manager *lease.Manager, config schedule.Config, requires ...foundation.ProviderID) foundation.Module {
	return schedule.Module("reports", SchedulerKey, config, requires, func(foundation.Resolver) (*lease.Manager, []schedule.Declaration, error) {
		declaration, err := ReportSchedule(reports)
		if err != nil {
			return nil, nil, err
		}
		options := schedule.DefaultOptions()
		options.WithoutOverlap = true
		declaration, err = declaration.With(options)
		return manager, []schedule.Declaration{declaration}, err
	})
}
