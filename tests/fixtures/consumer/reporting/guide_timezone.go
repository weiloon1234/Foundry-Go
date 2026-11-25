package reporting

import (
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/schedule"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

func zoneSettings() application.Settings {
	// doc-example:begin application-timezone-1
	settings := application.DefaultSettings()
	settings.TimeZone = "Asia/Kuala_Lumpur" // temporal.ZoneName
	// doc-example:end application-timezone-1
	return settings
}

func reportService(s application.Services) *Reports {
	// doc-example:begin application-timezone-2
	return NewReports(s.Time())
	// doc-example:end application-timezone-2
}

func calendarDates(s application.Services) error {
	// doc-example:begin application-timezone-3
	dates := s.Time()
	now, err := dates.Now() // UTC instant
	if err != nil {
		return err
	}
	today, err := dates.Today() // date in the application zone
	if err != nil {
		return err
	}
	when, err := dates.Parse("2026-09-26T09:00:00")
	if err != nil {
		return err
	}
	text, err := dates.Format(when) // 2026-09-26T09:00:00+08:00
	if err != nil {
		return err
	}
	start, err := dates.StartOfDay(when) // local midnight as a UTC instant
	if err != nil {
		return err
	}
	tomorrow, err := dates.AddDays(when, 1) // same local time next calendar day
	if err != nil {
		return err
	}
	// doc-example:end application-timezone-3
	_ = now
	_ = today
	_ = text
	_ = start
	_ = tomorrow
	return nil
}

func dailyReport(s application.Services, handler schedule.Handler) (schedule.Declaration, error) {
	// doc-example:begin application-timezone-4
	calendar := s.Calendar()
	return calendar.DailyAt("reports.daily", "09:00", handler)
	// doc-example:end application-timezone-4
}

// doc-example:begin report-dates-constructor
// Reports retains the application clock and zone for report dates.
type Reports struct{ dates temporal.Service }

// NewReports injects the configured temporal service.
func NewReports(dates temporal.Service) *Reports { return &Reports{dates: dates} }

// doc-example:end report-dates-constructor
