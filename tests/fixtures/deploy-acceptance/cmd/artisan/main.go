package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"foundry.test/deployacceptance/internal/app"
	"github.com/weiloon1234/Foundry-Go/cli"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	err := app.Run(ctx, os.Args[1:], cli.Streams{In: os.Stdin, Out: os.Stdout, Err: os.Stderr})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	os.Exit(int(cli.Status(err)))
}
