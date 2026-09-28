// Package testkit exercises the production Foundry lifecycle with test-owned
// cleanup. Feature-specific fakes and clients are added with those features.
package testkit

import (
	"context"
	"testing"

	"github.com/weiloon1234/Foundry-Go/foundation"
)

// Start builds and boots the supplied production bootstrap and registers cleanup
// before startup, so partial boot failures also release acquired resources.
func Start(t testing.TB, builder *foundation.Builder) *foundation.App {
	t.Helper()
	app, err := builder.Build(t.Context())
	if err != nil {
		t.Fatalf("build Foundry application: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), app.ShutdownTimeout())
		defer cancel()
		if err := app.Shutdown(ctx); err != nil {
			t.Errorf("shutdown Foundry application: %v", err)
		}
	})
	if err := app.Start(t.Context()); err != nil {
		t.Fatalf("start Foundry application: %v", err)
	}
	return app
}
