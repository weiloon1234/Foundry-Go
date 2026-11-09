package postgres_test

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/stdlib"
	foundry "github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	"github.com/weiloon1234/Foundry-Go/foundation"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func TestPostgresCloseAfterBackendTermination(t *testing.T) {
	control := pgtest.Open(t)
	bridge, disconnected := postgresTLSBridge(t, pgtest.Config(t))
	config := pgtest.Config(t)
	config.Pool.MaxOpen, config.Pool.MaxIdle = 2, 2
	bridge(&config)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	key := foundation.NewKey[*database.DB]("test.database")
	ready := make(chan struct{})
	kernel := foundation.Module{Name: "test.http", OnRegister: func(r *foundation.Registrar) error {
		return r.Kernel(foundation.HTTP, func(*foundation.Runtime) (foundation.Kernel, error) {
			return foundation.KernelFunc(func(ctx context.Context) error {
				close(ready)
				<-ctx.Done()
				return ctx.Err()
			}), nil
		})
	}}
	app, err := foundry.New().Register(postgres.Module("test.database", key, config), kernel).Build(ctx)
	if err != nil {
		t.Fatal(err)
	}
	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	result := make(chan error, 1)
	go func() { result <- app.Run(runCtx, foundation.HTTP) }()
	t.Cleanup(func() {
		stop()
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := app.Shutdown(cleanup); err != nil {
			t.Error(err)
		}
	})
	select {
	case <-ready:
	case err := <-result:
		t.Fatalf("application stopped before ready: %v", err)
	case <-ctx.Done():
		t.Fatal("application did not become ready")
	}
	db, err := foundation.Resolve(app.Services(), key)
	if err != nil {
		t.Fatal(err)
	}

	// Hold both rows open so each query owns a different physical connection.
	var held []*database.Rows
	defer func() {
		for _, rows := range held {
			_ = rows.Close()
		}
	}()
	var pids []int32
	for range 2 {
		rows, err := db.Query(ctx, "SELECT pg_catalog.pg_backend_pid()")
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, rows)
		var pid int32
		if !rows.Next() {
			t.Fatal("backend query returned no row")
		}
		if err := rows.Scan(&pid); err != nil {
			t.Fatal(err)
		}
		pids = append(pids, pid)
	}
	if pids[0] == pids[1] {
		t.Fatal("queries did not acquire distinct backends")
	}
	for _, rows := range held {
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if stats := db.Stats(); stats.Idle != 2 || stats.InUse != 0 || stats.Owners != 0 {
		t.Fatalf("expected two idle connections: %+v", stats)
	}
	for _, pid := range pids {
		var terminated bool
		// The timeout waits for termination; target only PIDs read from our pool.
		if err := database.ScanOne(ctx, control, "SELECT pg_catalog.pg_terminate_backend($1, 5000)", []any{pid}, &terminated); err != nil {
			t.Fatal(err)
		}
		if !terminated {
			t.Fatal("test-owned backend was not terminated")
		}
		select {
		case <-disconnected:
		case <-ctx.Done():
			t.Fatal("TLS bridge did not observe backend termination")
		}
	}
	// SIGTERM's signal.NotifyContext follows this cancellation path.
	stop()
	select {
	case err := <-result:
		if err != nil || cli.Status(err) != cli.Success {
			t.Fatalf("graceful application shutdown after backend termination: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("application did not finish shutdown")
	}
	for range 2 {
		if err := db.Close(ctx); err != nil {
			t.Fatalf("closing server-terminated idle connections: %v", err)
		}
	}
	select {
	case <-db.Done():
	default:
		t.Fatal("successful close did not finish pool cleanup")
	}
}

func TestPostgresClosePreservesTransportFailure(t *testing.T) {
	config := pgtest.Config(t)
	native, err := postgres.TestConnectionConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	cause := errors.New("test transport cleanup failed")
	dial := native.DialFunc
	native.DialFunc = func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := dial(ctx, network, address)
		if err != nil {
			return nil, err
		}
		return &closeFailureConnection{Conn: conn, cause: errors.Join(io.EOF, cause)}, nil
	}
	adapter, err := postgres.New(config)
	if err != nil {
		t.Fatal(err)
	}
	adapter.Connector = postgres.TestProtectedConnector(stdlib.GetConnector(*native))
	db, err := database.Open(t.Context(), adapter, config.Pool)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(context.Background())
	for range 2 {
		err := db.Close(t.Context())
		if !errors.Is(err, cause) || cli.Status(err) != cli.Failure {
			t.Fatal("genuine transport close failure was lost")
		}
	}
}

type closeFailureConnection struct {
	net.Conn
	cause error
}

func (c *closeFailureConnection) Close() error {
	return errors.Join(c.Conn.Close(), c.cause)
}
