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
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

var moduleDirectories = [...]string{".", "tests/fixtures/consumer", "tests/fixtures/goway"}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	goBinary := flag.String("go", "go", "selected Go command")
	file := flag.String("env-file", ".env.test", "private test environment file")
	race := flag.Bool("race", false, "enable the race detector")
	exempt := flag.String("race-exempt", "", "space-separated race-exempt import paths; configured by Makefile")
	heavy := flag.String("heavy", "", "space-separated heavy import paths, heaviest first; configured by Makefile")
	shardSpec := flag.String("shard", "1/1", "package shard INDEX/TOTAL (1-based)")
	batchSize := flag.Int("batch-size", 0, "required maximum packages per test invocation; configured by Makefile")
	timeout := flag.Duration("timeout", 0, "required positive per-package test timeout; configured by Makefile")
	flag.Parse()
	if err := run(ctx, *goBinary, *file, *race, *batchSize, *timeout, *shardSpec, strings.Fields(*exempt), strings.Fields(*heavy)); err != nil {
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

func run(ctx context.Context, goBinary, file string, race bool, batchSize int, timeout time.Duration, shardSpec string, exempt, heavy []string) error {
	if batchSize <= 0 {
		return errors.New("PostgreSQL test package batch size must be positive")
	}
	if timeout <= 0 {
		return errors.New("PostgreSQL test package timeout must be positive")
	}
	shard, err := parseShard(shardSpec)
	if err != nil {
		return err
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
	if !race {
		exempt = nil
	} else if len(exempt) > 0 {
		fmt.Printf("Race-exempt (covered by make test): %s\n", strings.Join(exempt, " "))
	}
	for _, dir := range moduleDirectories {
		args := []string{"test", "-mod=readonly", "-count=1", "-timeout=" + timeout.String()}
		if race {
			args = append(args, "-race")
		}
		if err := testPackages(ctx, goBinary, dir, environment, args, batchSize, shard, exempt, heavy); err != nil {
			return fmt.Errorf("required PostgreSQL acceptance failed in %s: %w", dir, err)
		}
	}
	return nil
}

type packageShard struct {
	index int
	total int
}

func parseShard(value string) (packageShard, error) {
	indexText, totalText, ok := strings.Cut(value, "/")
	index, indexErr := strconv.Atoi(indexText)
	total, totalErr := strconv.Atoi(totalText)
	if !ok || indexErr != nil || totalErr != nil || index < 1 || total < 1 || index > total {
		return packageShard{}, errors.New("shard must be INDEX/TOTAL with 1 <= INDEX <= TOTAL")
	}
	return packageShard{index: index, total: total}, nil
}

func partitionPackages(packages []string, shard packageShard, exempt, heavy []string) []string {
	packages = slices.Clone(packages)
	slices.Sort(packages)
	selected := make([]string, 0)
	if shard.total > 1 {
		// Keep list positions across modules so fixture roots do not both start
		// on shard one. Missing and exempt packages are never selected.
		for index, pkg := range heavy {
			if slices.Index(heavy, pkg) != index || !slices.Contains(packages, pkg) || slices.Contains(exempt, pkg) {
				continue
			}
			if index%shard.total == shard.index-1 {
				selected = append(selected, pkg)
			}
		}
	}
	index := 0
	for _, pkg := range packages {
		if slices.Contains(exempt, pkg) || shard.total > 1 && slices.Contains(heavy, pkg) {
			continue
		}
		if index%shard.total == shard.index-1 {
			selected = append(selected, pkg)
		}
		index++
	}
	return selected
}

func listPackages(ctx context.Context, goBinary, dir string, environment []string) ([]string, error) {
	list := exec.CommandContext(ctx, goBinary, "list", "-mod=readonly", "./...")
	list.Dir, list.Env, list.Stderr = dir, environment, os.Stderr
	output, err := list.Output()
	if err != nil {
		return nil, fmt.Errorf("discover test packages: %w", err)
	}
	packages := strings.Fields(string(output))
	if len(packages) == 0 {
		return nil, errors.New("test package discovery returned no packages")
	}
	return packages, nil
}

func testPackages(ctx context.Context, goBinary, dir string, environment, args []string, batchSize int, shard packageShard, exempt, heavy []string) error {
	if batchSize <= 0 {
		return errors.New("test package batch size must be positive")
	}
	packages, err := listPackages(ctx, goBinary, dir, environment)
	if err != nil {
		return err
	}
	packages = partitionPackages(packages, shard, exempt, heavy)
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
