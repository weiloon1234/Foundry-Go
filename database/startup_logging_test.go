package database_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	foundry "github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

func TestStartupLogsRetriesAndReadPoolWithoutPrivateCause(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	var attempts atomic.Int32
	primary := &driverState{connect: func(context.Context) error {
		if attempts.Add(1) < 3 {
			return errors.New("private-connect-string-password")
		}
		return nil
	}}
	read := &driverState{}
	config := database.DefaultPoolConfig()
	adapter := database.Adapter{Connector: connector{primary}, Classify: func(error) database.Detail { return database.Detail{Code: database.Unavailable, SQLState: "57P03"} }}
	db, err := database.Open(t.Context(), adapter, config, database.WithStartupLog(logger), database.WithReadPool(func() (database.Adapter, error) { return database.Adapter{Connector: connector{read}}, nil }, config, config.MaxOpen*2))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(context.Background())
	records := startupRecords(t, logs.String())
	retries, ready := 0, map[string]bool{}
	for _, record := range records {
		switch record["msg"] {
		case "database startup retry":
			retries++
			if record["attempt"] != float64(retries) || record["code"] != "unavailable" || record["sqlstate"] != "57P03" || record["retry_delay"].(float64) <= 0 {
				t.Fatal("retry classification missing", record)
			}
		case "database startup ready":
			ready[record["role"].(string)] = true
		}
	}
	if retries != 2 || !ready["primary"] || !ready["read"] || strings.Contains(logs.String(), "private-connect") {
		t.Fatal("incomplete or unsafe diagnostics", logs.String())
	}
}

func TestStartupFailureAndCancellationRemainTerminal(t *testing.T) {
	for _, mode := range []string{"authentication", "budget", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			var logs bytes.Buffer
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var attempts, classifications atomic.Int32
			state := &driverState{connect: func(context.Context) error { attempts.Add(1); return errors.New("private-driver-message") }}
			config := database.DefaultPoolConfig()
			detail := database.Detail{Code: database.Unavailable, SQLState: "57P03"}
			if mode == "authentication" {
				detail.SQLState = "28P01"
			}
			if mode == "budget" {
				config.StartupTimeout = 0
			}
			handler := slog.NewJSONHandler(&logs, nil)
			logger := slog.New(startupHandler{Handler: handler, handle: func(record slog.Record) {
				if mode == "cancel" && record.Message == "database startup retry" {
					cancel()
				}
			}})
			db, err := database.Prepare(database.Adapter{Connector: connector{state}, Classify: func(error) database.Detail { classifications.Add(1); return detail }}, config, database.WithStartupLog(logger))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close(context.Background())
			if err = db.Start(ctx); err == nil || attempts.Load() != 1 || db.Stats().Ready || db.Stats().Owners != 0 {
				t.Fatal("startup failure ownership changed", err)
			}
			if mode == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation lost", err)
			}
			want := int32(1)
			if mode == "cancel" {
				want = 2
			}
			if classifications.Load() != want {
				t.Fatal("logging repeated driver classification", classifications.Load())
			}
			records := startupRecords(t, logs.String())
			if len(records) < 2 || !strings.HasPrefix(records[len(records)-1]["msg"].(string), "database startup ") || strings.Contains(logs.String(), "private-driver") {
				t.Fatal("missing safe terminal diagnostic")
			}
		})
	}
}

func TestStartupLoggerOptionsRejectBeforeConnecting(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	for _, options := range [][]database.Option{{database.WithStartupLog(nil)}, {database.WithStartupLog(logger), database.WithStartupLog(logger)}} {
		state := &driverState{}
		if db, err := database.Prepare(database.Adapter{Connector: connector{state}}, database.DefaultPoolConfig(), options...); err == nil || db != nil || state.connected.Load() != 0 {
			t.Fatal("invalid logging options acquired resources")
		}
	}
}

type startupHandler struct {
	slog.Handler
	handle func(slog.Record)
}

func (h startupHandler) Handle(ctx context.Context, record slog.Record) error {
	h.handle(record)
	return h.Handler.Handle(ctx, record)
}

func TestStartupLoggerFailuresCannotStrandPool(t *testing.T) {
	for _, exit := range []bool{false, true} {
		t.Run(map[bool]string{false: "panic", true: "goexit"}[exit], func(t *testing.T) {
			logger := slog.New(startupHandler{Handler: slog.NewTextHandler(&bytes.Buffer{}, nil), handle: func(slog.Record) {
				if exit {
					runtime.Goexit()
				}
				panic("private-log-panic")
			}})
			db, err := database.Open(t.Context(), database.Adapter{Connector: connector{&driverState{}}}, database.DefaultPoolConfig(), database.WithStartupLog(logger))
			if err != nil {
				t.Fatal(err)
			}
			if !db.Stats().Ready || db.Stats().Owners != 0 {
				t.Fatal("logger changed lifecycle")
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if err := db.Close(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestModuleBindsNamedStartupLogger(t *testing.T) {
	var logs bytes.Buffer
	key := foundation.NewKey[*database.DB]("test.startup")
	module := database.Module("test.startup", key, func() (database.Adapter, error) { return database.Adapter{Connector: connector{&driverState{}}}, nil }, database.DefaultPoolConfig())
	app, err := foundry.New(foundation.WithLogger(slog.New(slog.NewJSONHandler(&logs, nil)))).Register(module).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer app.Shutdown(context.Background())
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), `"database":"test.startup"`) || !strings.Contains(logs.String(), "database startup ready") {
		t.Fatal("module did not inherit named logger", logs.String())
	}
}

func startupRecords(t *testing.T, text string) []map[string]any {
	t.Helper()
	var result []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		result = append(result, record)
	}
	return result
}
