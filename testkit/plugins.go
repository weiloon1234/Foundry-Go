package testkit

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/foundation"
)

// Plugins runs explicitly linked plugins through production Build/Start and the
// existing test-owned cleanup. For application provider dependencies or custom
// runtime options use Start with a fully assembled builder instead.
func Plugins(t testing.TB, plugins ...foundation.Plugin) *foundation.App {
	t.Helper()
	return Start(t, foundation.NewBuilder().RegisterPlugin(plugins...))
}
