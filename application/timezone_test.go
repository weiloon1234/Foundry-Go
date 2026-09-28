package application

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

func TestReportTimeZoneInheritsApplicationUnlessExplicit(t *testing.T) {
	s := DefaultSettings()
	s.TimeZone = "Asia/Kuala_Lumpur"
	s.Services.Database.Connections = infrastructure.DatabaseConnections{"default": infrastructure.DefaultConnectionSettings()}
	s.Features.Reports.Enabled = true
	for _, tc := range []struct{ setting, want temporal.ZoneName }{
		{"", "Asia/Kuala_Lumpur"}, {temporal.UTC, temporal.UTC},
	} {
		s.Features.Reports.Config.TimeZone = tc.setting
		features, err := prepareFeatureSettings(s, clock.System{})
		if err != nil || features.Reports.Config.TimeZone != tc.want {
			t.Fatal(features.Reports.Config.TimeZone, err)
		}
		if s.Features.Reports.Config.TimeZone != tc.setting {
			t.Fatal("input settings changed")
		}
	}
}
