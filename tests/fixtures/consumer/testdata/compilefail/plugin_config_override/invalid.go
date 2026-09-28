package compilefail

import (
	"foundry.test/pluginbase"
	"github.com/weiloon1234/Foundry-Go/config"
)

type Other struct{ Value string }

var other = config.String("plugins.other.value", func(v *Other) *string { return &v.Value })
var _ = config.Inputs[pluginbase.Settings]{Overrides: []config.Override[pluginbase.Settings]{other.Set("wrong owner")}}
