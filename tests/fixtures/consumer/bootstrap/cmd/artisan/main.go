package main

import (
	"context"
	"fmt"
	"os"

	"foundry.test/consumer/bootstrap"
	"github.com/weiloon1234/Foundry-Go/cli"
)

func main() {
	err := bootstrap.RunDeployJobs(context.Background(), os.Args[1:], cli.Streams{In: os.Stdin, Out: os.Stdout, Err: os.Stderr})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	os.Exit(int(cli.Status(err)))
}
