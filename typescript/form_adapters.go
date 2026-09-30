package typescript

import (
	_ "embed"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/generate"
)

//go:embed adapters/react.ts
var reactAdapter string

//go:embed adapters/vue.ts
var vueAdapter string

func renderFormAdapter(prefix, source string) []byte {
	return []byte(generate.ArtifactHeader + "\n" + strings.ReplaceAll(source, "./contracts_foundry.gen.js", "./"+prefix+"_foundry.gen.js"))
}
