package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"foundry.test/consumer/configuredprofile"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	path := flag.String("config", "", "optional TOML configuration")
	serve := flag.Bool("serve", false, "serve until interrupted; otherwise run one HTTP smoke check")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	settings, err := configuredprofile.Load(*path)
	if err != nil {
		return err
	}
	app, err := configuredprofile.Build(ctx, settings)
	if err != nil {
		return err
	}
	if *serve {
		err := app.Run(ctx, foundation.HTTP)
		if errors.Is(err, context.Canceled) {
			cleanup, cancel := context.WithTimeout(context.Background(), app.ShutdownTimeout())
			defer cancel()
			return app.Shutdown(cleanup)
		}
		return err
	}
	return configuredprofile.Smoke(ctx, app)
}
