package application_test

import (
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/config/toml"
	"strings"
	"testing"
	"time"
)

func TestSupportingConfigurationUsesGeneratedKeysAndNamedElementSchemas(t *testing.T) {
	schema, err := application.SettingsConfigSchema()
	if err != nil {
		t.Fatal(err)
	}
	keys := application.SettingsConfigKeys()
	document := `[services.mail.mailers.default]
driver='memory'
[services.mail.mailers.default.config]
from='Configured <configured@example.test>'
timeout='9s'
[services.jobs.connections.default]
default_queue='reports'
[services.http_clients.clients.default.config]
base_url='http://127.0.0.1:8080'
[services.http_clients.clients.default.config.headers]
X-Client=['fixture']
[services.pub_sub.connections.default]
driver='memory'
[services.realtime.connections.default]
driver='local'
[log]
default='stack'
[log.channels.stack.sink]
driver='stack'
[log.channels.stack]
stack=['console']
[log.channels.console.sink]
driver='stderr'
[features.auth.browser.guards.web]
cookie='__Host-configured'
`
	layer, err := toml.Decode(strings.NewReader(document), schema, toml.Options{Name: "configured"})
	if err != nil {
		t.Fatal(err)
	}
	settings, _, err := schema.Load(application.DefaultSettings(), config.Inputs[application.Settings]{Files: []config.Values{layer}, Overrides: []config.Override[application.Settings]{keys.Worker.Config.PollInterval.Set(25 * time.Millisecond), keys.Features.Health.Enabled.Set(true)}})
	if err != nil {
		t.Fatal(err)
	}
	if settings.Services.Mail.Mailers["default"].Config.From.Mailbox() != "configured@example.test" || settings.Services.Jobs.Connections["default"].DefaultQueue != "reports" || settings.Worker.Config.PollInterval != 25*time.Millisecond || !settings.Features.Health.Enabled {
		t.Fatal("nested generated configuration lost typed values")
	}
	settings.HTTP.Enabled = false
	app, err := application.New(settings, quiet()).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(t, app) })
}
