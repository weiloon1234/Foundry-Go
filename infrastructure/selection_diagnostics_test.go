package infrastructure_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
)

func TestDefaultSelectionErrorsIdentifyTheirGeneratedSetting(t *testing.T) {
	fields := infrastructure.SettingsConfigKeys()
	for _, family := range []struct {
		field     string
		configure func(*infrastructure.Settings, bool)
	}{
		{fields.Database.Default.Name(), func(s *infrastructure.Settings, invalid bool) {
			s.Database.Connections = infrastructure.DatabaseConnections{"registered": {}}
			s.Database.Default = "missing"
			if invalid {
				s.Database.Default = "private\nvalue"
			}
		}},
		{fields.Redis.Default.Name(), func(s *infrastructure.Settings, invalid bool) {
			s.Redis.Connections = infrastructure.RedisConnections{"registered": {}}
			s.Redis.Default = "missing"
			if invalid {
				s.Redis.Default = "private\nvalue"
			}
		}},
		{fields.Storage.Default.Name(), func(s *infrastructure.Settings, invalid bool) {
			s.Storage.Disks = infrastructure.Disks{"registered": {}}
			s.Storage.Default = "missing"
			if invalid {
				s.Storage.Default = "private\nvalue"
			}
		}},
		{fields.Cache.Default.Name(), func(s *infrastructure.Settings, invalid bool) {
			s.Cache.Stores = infrastructure.CacheStores{"registered": {}}
			s.Cache.Default = "missing"
			if invalid {
				s.Cache.Default = "private\nvalue"
			}
		}},
		{fields.Mail.Default.Name(), func(s *infrastructure.Settings, invalid bool) {
			s.Mail.Mailers = infrastructure.Mailers{"registered": {}}
			s.Mail.Default = "missing"
			if invalid {
				s.Mail.Default = "private\nvalue"
			}
		}},
		{fields.Jobs.Default.Name(), func(s *infrastructure.Settings, invalid bool) {
			s.Jobs.Connections = infrastructure.JobConnections{"registered": {}}
			s.Jobs.Default = "missing"
			if invalid {
				s.Jobs.Default = "private\nvalue"
			}
		}},
		{fields.HTTPClients.Default.Name(), func(s *infrastructure.Settings, invalid bool) {
			s.HTTPClients.Clients = infrastructure.HTTPClients{"registered": {}}
			s.HTTPClients.Default = "missing"
			if invalid {
				s.HTTPClients.Default = "private\nvalue"
			}
		}},
		{fields.PubSub.Default.Name(), func(s *infrastructure.Settings, invalid bool) {
			s.PubSub.Connections = infrastructure.Brokers{"registered": {}}
			s.PubSub.Default = "missing"
			if invalid {
				s.PubSub.Default = "private\nvalue"
			}
		}},
		{fields.Realtime.Default.Name(), func(s *infrastructure.Settings, invalid bool) {
			s.Realtime.Connections = infrastructure.RealtimeConnections{"registered": {}}
			s.Realtime.Default = "missing"
			if invalid {
				s.Realtime.Default = "private\nvalue"
			}
		}},
	} {
		t.Run(family.field, func(t *testing.T) {
			for _, invalid := range []bool{false, true} {
				settings := infrastructure.DefaultSettings()
				family.configure(&settings, invalid)
				plan, err := infrastructure.Configure(settings)
				want := fault.Missing
				if invalid {
					want = fault.Invalid
				}
				if plan != nil || !errors.Is(err, want) {
					t.Fatalf("selection returned a plan or lost its classification: %v", err)
				}
				if !strings.Contains(err.Error(), family.field) || strings.Contains(err.Error(), "private") {
					t.Fatalf("selection diagnostic lacks field context or exposes input: %v", err)
				}
			}
		})
	}
}
