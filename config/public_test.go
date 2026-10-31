package config_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/config"
)

func TestPublicSettingOptionalJSON(t *testing.T) {
	for _, optional := range []bool{false, true} {
		setting := config.PublicSetting{Name: "timeZone", Env: "VITE_TIME_ZONE", Kind: config.PublicTimeZone, Optional: optional}
		data, err := json.Marshal(setting)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), `"optional":true`) != optional || strings.Contains(string(data), `"optional":false`) {
			t.Fatalf("unexpected optional metadata: %s", data)
		}
		var restored config.PublicSetting
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatal(err)
		}
		if restored.Optional != optional {
			t.Fatal("optional metadata changed during serialization")
		}
	}
}
