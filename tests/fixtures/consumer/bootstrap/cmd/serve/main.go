package main

import (
	"context"
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
	return runHTTP(ctx, *path)
}

func runHTTP(ctx context.Context, configPath string) error {
	// doc-example:begin application-bootstrap-1
	settings, err := bootstrap.Load(configPath)
	if err != nil {
		return err
	}
	app, err := bootstrap.Build(ctx, settings)
	if err != nil {
		return err
	}
	// A signal is a graceful stop: Run drains within the application shutdown
	// budget and returns nil, or the task/cleanup failures it observed.
	runErr := app.Run(ctx, foundation.HTTP)
	// doc-example:end application-bootstrap-1
	return runErr
}
