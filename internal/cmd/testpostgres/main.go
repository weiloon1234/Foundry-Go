// Command testpostgres runs required PostgreSQL acceptance without treating an
// environment file as executable shell code or exposing its connection string.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	goBinary := flag.String("go", "go", "selected Go command")
	file := flag.String("env-file", ".env.test", "private test environment file")
	race := flag.Bool("race", false, "enable the race detector")
	batchSize := flag.Int("batch-size", 0, "required maximum packages per test invocation; configured by Makefile")
	timeout := flag.Duration("timeout", 0, "required positive per-package test timeout; configured by Makefile")
	flag.Parse()
	if err := run(ctx, *goBinary, *file, *race, *batchSize, *timeout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func readURL(path string) (string, error) {
	if value := os.Getenv(pgtest.URLVariable); value != "" {
		return value, nil
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", errors.New("set FOUNDRY_TEST_POSTGRES_URL or provide the private .env.test file")
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return "", errors.New("PostgreSQL test environment file must be a private regular file (mode 0600)")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", errors.New("cannot open private PostgreSQL test configuration")
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) || !opened.Mode().IsRegular() || opened.Mode().Perm()&0077 != 0 {
		return "", errors.New("private PostgreSQL test configuration changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(file, (64<<10)+1))
	if err != nil || len(data) > 64<<10 {
		return "", errors.New("cannot read bounded PostgreSQL test configuration")
	}
	var value string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, raw, ok := strings.Cut(line, "=")
		if !ok || key != pgtest.URLVariable || value != "" || raw == "" {
			return "", errors.New("test environment file must contain one FOUNDRY_TEST_POSTGRES_URL=value assignment")
		}
		value = raw
	}
	if value == "" {
		return "", errors.New("required PostgreSQL test URL is missing")
	}
	return value, nil
}

func run(ctx context.Context, goBinary, file string, race bool, batchSize int, timeout time.Duration) error {
	if batchSize <= 0 {
		return errors.New("PostgreSQL test package batch size must be positive")
	}
	if timeout <= 0 {
		return errors.New("PostgreSQL test package timeout must be positive")
	}
	value, err := readURL(file)
	if err != nil {
		return err
	}
	var environment []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key != pgtest.URLVariable && key != pgtest.RequiredVariable && key != "GOWORK" {
			environment = append(environment, entry)
		}
	}
	environment = append(environment, pgtest.URLVariable+"="+value, pgtest.RequiredVariable+"=1", "GOWORK=off")
	for _, dir := range []string{".", "tests/fixtures/consumer"} {
		args := []string{"test", "-mod=readonly", "-count=1", "-timeout=" + timeout.String()}
		if race {
			args = append(args, "-race")
		}
		if err := testPackages(ctx, goBinary, dir, environment, args, batchSize); err != nil {
			return fmt.Errorf("required PostgreSQL acceptance failed in %s: %w", dir, err)
		}
	}
	return nil
}

func testPackages(ctx context.Context, goBinary, dir string, environment, args []string, batchSize int) error {
	if batchSize <= 0 {
		return errors.New("test package batch size must be positive")
	}
	list := exec.CommandContext(ctx, goBinary, "list", "-mod=readonly", "./...")
	list.Dir, list.Env, list.Stderr = dir, environment, os.Stderr
	output, err := list.Output()
	if err != nil {
		return fmt.Errorf("discover test packages: %w", err)
	}
	packages := strings.Fields(string(output))
	if len(packages) == 0 {
		return errors.New("test package discovery returned no packages")
	}
	for len(packages) > 0 {
		count := min(batchSize, len(packages))
		batch := packages[:count]
		cmd := exec.CommandContext(ctx, goBinary, append(args[:len(args):len(args)], batch...)...)
		cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = dir, environment, os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			return err
		}
		packages = packages[count:]
	}
	return nil
}
