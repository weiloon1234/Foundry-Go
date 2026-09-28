package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"foundry.test/consumer/bootstrap"
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
	path := flag.String("config", "", "optional TOML configuration path")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	settings, err := bootstrap.Load(*path)
	if err != nil {
		return err
	}
	app, err := bootstrap.Build(ctx, settings)
	if err != nil {
		return err
	}
	err = app.Run(ctx, foundation.HTTP)
	if errors.Is(err, context.Canceled) {
		// Shutdown retains task/cleanup failures independently of caller cancellation.
		cleanup, finish := context.WithTimeout(context.Background(), app.ShutdownTimeout())
		defer finish()
		return app.Shutdown(cleanup)
	}
	return err
}
