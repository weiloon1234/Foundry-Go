package database_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func readOption(state *driverState, config database.PoolConfig, limit int) database.Option {
	return database.WithReadPool(func() (database.Adapter, error) { return database.Adapter{Connector: connector{state}}, nil }, config, limit)
}

func routedPool(t *testing.T, primary, read *driverState) *database.DB {
	t.Helper()
	config := database.DefaultPoolConfig()
	config.MaxOpen, config.MaxIdle = 2, 1
	db, err := database.Open(t.Context(), database.Adapter{Connector: connector{primary}}, config, readOption(read, config, 4))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := db.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return db
}

type routedRecord struct{ ID int64 }

func routedQuery() query.Query[routedRecord] {
	return query.ForModel(query.Define("routed_records", "id", []query.Column{{Name: "id"}}, func(row database.Row) (routedRecord, error) {
		var item routedRecord
		err := row.Scan(&item.ID)
		return item, err
	}))
}

func TestReadRoutingPreservesRawPrimaryAndActualTransaction(t *testing.T) {
	primary, read := &driverState{}, &driverState{}
	var primaryQueries, readQueries, primaryExecs atomic.Int64
	makeQuery := func(state *driverState, count *atomic.Int64, id int64) func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
		return func(ctx context.Context, statement string, _ []driver.NamedValue) (driver.Rows, error) {
			count.Add(1)
			var result driver.Value = id
			if strings.Contains(statement, "EXISTS") {
				result = true
			}
			return &resultRows{state: state, values: [][]driver.Value{{result}}}, ctx.Err()
		}
	}
	primary.query, read.query = makeQuery(primary, &primaryQueries, 11), makeQuery(read, &readQueries, 22)
	primary.exec = func(context.Context, string, []driver.NamedValue) (driver.Result, error) {
		primaryExecs.Add(1)
		return driver.RowsAffected(1), nil
	}
	db := routedPool(t, primary, read)
	var raw int64
	if err := database.ScanOne(t.Context(), db, "INSERT INTO records VALUES (1) RETURNING id", nil, &raw); err != nil || raw != 11 {
		t.Fatal("raw RETURNING changed routing", err)
	}
	q := routedQuery()
	rows, err := q.All(t.Context(), db)
	if err != nil || len(rows) != 1 || rows[0].ID != 22 {
		t.Fatal("SELECT did not route to configured read pool", err)
	}
	if n, err := q.Count(t.Context(), db); err != nil || n != 22 {
		t.Fatal("count did not route to read pool", err)
	}
	if exists, err := q.Exists(t.Context(), db); err != nil || !exists {
		t.Fatal("exists failed", err)
	}
	if err := q.Each(t.Context(), db, func(item routedRecord) error {
		if item.ID != 22 {
			return errors.New("stream escaped read routing")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	rows, err = q.All(t.Context(), db.Primary())
	if err != nil || len(rows) != 1 || rows[0].ID != 11 {
		t.Fatal("explicit primary read was routed to replica", err)
	}
	readsBefore := readQueries.Load()
	if err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		items, err := q.ForUpdate().All(t.Context(), tx)
		if err != nil {
			return err
		}
		if len(items) != 1 || items[0].ID != 11 {
			return errors.New("locked read escaped transaction")
		}
		return tx.Transaction(t.Context(), func(nested *database.Tx) error {
			_, err := q.All(t.Context(), nested)
			return err
		})
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Session(t.Context(), func(session *database.Session) error {
		items, err := q.All(t.Context(), session)
		if err != nil {
			return err
		}
		if len(items) != 1 || items[0].ID != 11 {
			return errors.New("session read escaped its connection")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if readQueries.Load() != readsBefore || read.committed.Load() != 0 || primary.committed.Load() != 1 {
		t.Fatal("transaction or session used replica")
	}
	if _, err := db.Exec(t.Context(), "UPDATE records SET id=1"); err != nil || primaryExecs.Load() == 0 {
		t.Fatal("write was not primary owned", err)
	}
	if primaryQueries.Load() < 5 || db.Stats().Owners != 0 {
		t.Fatal("routing lost execution or resource accounting")
	}
}

func TestReadPoolFailureDoesNotFallbackAndHealthNamesBothEndpoints(t *testing.T) {
	primary, read := &driverState{}, &driverState{}
	var unavailable atomic.Bool
	read.ping = func(context.Context) error {
		if unavailable.Load() {
			return driver.ErrBadConn
		}
		return nil
	}
	read.query = func(context.Context, string, []driver.NamedValue) (driver.Rows, error) { return nil, driver.ErrBadConn }
	db := routedPool(t, primary, read)
	unavailable.Store(true)
	if _, err := routedQuery().All(t.Context(), db); err == nil {
		t.Fatal("unavailable replica silently fell back")
	}
	if _, err := routedQuery().All(t.Context(), db.Primary()); err != nil {
		t.Fatal("primary unusable after replica query failure", err)
	}
	health := db.Health(t.Context())
	if len(health) != 2 || health[0].Role != database.PrimaryPool || health[0].Error != nil || health[1].Role != database.ReadPool || health[1].Error == nil {
		t.Fatal("endpoint health was collapsed", health)
	}
	if db.Ping(t.Context()) == nil || db.PingPrimary(t.Context()) != nil {
		t.Fatal("combined readiness ignored replica failure")
	}
}

func TestReadPoolStartupFailureClosesBothAndNeverBecomesReady(t *testing.T) {
	primary := &driverState{}
	read := &driverState{ping: func(context.Context) error { return errors.New("private endpoint credential") }}
	config := database.DefaultPoolConfig()
	db, err := database.Prepare(database.Adapter{Connector: connector{primary}}, config, readOption(read, config, 32))
	if err != nil {
		t.Fatal(err)
	}
	err = db.Start(t.Context())
	if err == nil || strings.Contains(err.Error(), "private endpoint credential") || db.Stats().Ready {
		t.Fatal("partial startup published ready or exposed cause", err)
	}
	if primary.closed.Load() != primary.connected.Load() || read.closed.Load() != read.connected.Load() {
		t.Fatal("partial startup leaked endpoint connections")
	}
	if err := db.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.QueryRead(t.Context(), "SELECT 1"); !errors.Is(err, database.Closed) {
		t.Fatal("closed failed startup accepted work", err)
	}
}

func TestReadPoolBoundsArePureAndShutdownDrainsBothEndpoints(t *testing.T) {
	primary, read := &driverState{}, &driverState{}
	config := database.DefaultPoolConfig()
	adapter := database.Adapter{Connector: connector{primary}}
	for _, options := range [][]database.Option{
		{readOption(read, config, 31)},
		{readOption(read, config, 32), readOption(read, config, 32)},
		{readOption(read, config, 32), database.WithConnectionLimit(31)},
		{database.WithConnectionLimit(15)},
		{database.WithConnectionLimit(16), database.WithConnectionLimit(16)},
	} {
		if db, err := database.Prepare(adapter, config, options...); !errors.Is(err, fault.Invalid) || db != nil {
			t.Fatal("invalid combined bounds accepted", err)
		}
	}
	if primary.connected.Load() != 0 || read.connected.Load() != 0 {
		t.Fatal("pure validation connected")
	}
	db := routedPool(t, primary, read)
	primaryRows, err := db.Query(t.Context(), "SELECT 1")
	if err != nil {
		t.Fatal(err)
	}
	defer primaryRows.Close()
	readRows, err := db.QueryRead(t.Context(), "SELECT 1")
	if err != nil {
		t.Fatal(err)
	}
	defer readRows.Close()
	stats := db.RoutingStats()
	readStats, configured := stats.Read.Get()
	if !configured || stats.Primary.InUse != 1 || readStats.InUse != 1 || stats.MaxConnections != 4 || db.Stats().Owners != 2 || db.Stats().InUse != 2 {
		t.Fatal("per-pool and combined resource accounting diverged")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := db.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("close did not retain unfinished owners", err)
	}
	if _, err := db.QueryRead(t.Context(), "SELECT 1"); !errors.Is(err, database.Closed) {
		t.Fatal("shutdown accepted read work", err)
	}
	if err := primaryRows.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-db.Done():
		t.Fatal("shutdown forgot replica rows")
	default:
	}
	if err := readRows.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if primary.closed.Load() != primary.connected.Load() || read.closed.Load() != read.connected.Load() || db.Stats().Owners != 0 {
		t.Fatal("shutdown leaked a pool")
	}
}

func TestUnconfiguredReadPoolUsesPrimary(t *testing.T) {
	db := open(t, &driverState{}, nil)
	items, err := routedQuery().All(t.Context(), db)
	if err != nil || len(items) != 1 || items[0].ID != 42 || db.HasReadPool() || db.RoutingStats().Read.IsSet() {
		t.Fatal("single-pool behavior changed", err)
	}
	if len(db.Health(t.Context())) != 1 || db.PingRead(t.Context()) != nil {
		t.Fatal("single-pool health changed")
	}
}
