package security

import (
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"net/netip"
	"testing"
)

func TestGeneratedDestinationSettingsRemainTyped(t *testing.T) {
	schema, err := infrastructure.HTTPClientSettingsConfigSchema()
	if err != nil {
		t.Fatal(err)
	}
	networks := []netip.Prefix{netip.MustParsePrefix("10.20.0.0/16")}
	keys := infrastructure.HTTPClientSettingsConfigKeys()
	settings, _, err := schema.Load(PreviewSettings(), config.Inputs[infrastructure.HTTPClientSettings]{Overrides: []config.Override[infrastructure.HTTPClientSettings]{keys.Config.Destination.Networks.Set(networks)}})
	if err != nil {
		t.Fatal(err)
	}
	networks[0] = netip.MustParsePrefix("127.0.0.0/8")
	if settings.Config.Destination.Networks[0].String() != "10.20.0.0/16" {
		t.Fatal("generated setting aliases caller state")
	}
	if err := settings.Config.Validate(); err != nil {
		t.Fatal(err)
	}
}
