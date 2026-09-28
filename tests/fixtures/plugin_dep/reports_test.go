package plugindep_test

import (
	"errors"
	"net/http/httptest"
	"testing"

	"foundry.test/pluginbase"
	"foundry.test/plugindep"
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

func TestIndependentDependentPluginUsesPublicContributions(t *testing.T) {
	base, err := pluginbase.New(config.Inputs[pluginbase.Settings]{})
	if err != nil {
		t.Fatal(err)
	}
	reports, err := plugindep.New()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := foundation.NewBuilder().RegisterPlugin(reports).Build(t.Context()); !errors.Is(err, fault.Missing) {
		t.Fatal("missing dependency accepted", err)
	}
	app := testkit.Plugins(t, reports, base)
	router, err := foundation.Resolve(app.Services(), pluginbase.Router)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/reports", nil))
	if response.Code != 200 || response.Body.String() != "base reports" || response.Header().Get("X-Plugin") != "reports" {
		t.Fatal("independent plugin route", response.Result())
	}
	registry, err := foundation.Resolve(app.Services(), pluginbase.Authorization)
	if err != nil {
		t.Fatal(err)
	}
	if err := plugindep.CanRead.ValidateIn(registry); err != nil {
		t.Fatal(err)
	}
}
