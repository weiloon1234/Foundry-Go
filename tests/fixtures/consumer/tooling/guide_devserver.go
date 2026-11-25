package tooling

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/devserver"
)

// doc-example:begin developer-tooling-and-testing-1
func runDevelopmentServer(ctx context.Context, projectRoot, localEnvPath string, streams cli.Streams, validatePortals func() error) error {
	opts := devserver.Options{
		Root:    projectRoot,
		Target:  "./cmd/artisan",
		Args:    []string{"serve"},
		Env:     []string{"WEB__ENABLED=true", "WEB__DEV_PROXY=true"},
		Watched: []string{"config/*.toml", "web/portals/*/portal.json", localEnvPath},
		Helpers: []devserver.Helper{
			{
				Name:       "admin",
				Executable: "npm",
				Args:       []string{"--prefix", "web", "run", "dev", "--", "admin", "--port", "5173", "--strictPort"},
				Banner:     "admin: http://127.0.0.1:8080/admin (Vite on 5173)",
			},
		},
		OnChange: validatePortals,
	}
	if err := devserver.Run(ctx, opts, streams); err != nil {
		return err
	}
	return nil
}

// doc-example:end developer-tooling-and-testing-1
