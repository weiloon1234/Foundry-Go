package observerqueries_test

import (
	"context"
	"errors"
	"testing"

	"foundry.test/consumer/observerqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
)

type forwardingReader struct {
	inner   database.Executor
	streams *[]*database.Rows
}

func (r forwardingReader) Exec(ctx context.Context, sql string, args ...any) (database.Result, error) {
	return r.inner.Exec(ctx, sql, args...)
}
func (r forwardingReader) Query(ctx context.Context, sql string, args ...any) (*database.Rows, error) {
	rows, err := r.inner.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	if err := checkReadObservers(rows); err != nil {
		return nil, errors.Join(err, rows.Close())
	}
	*r.streams = append(*r.streams, rows)
	return rows, nil
}

func checkReadObservers(rows *database.Rows) error {
	set := rows.Observers()
	records := lifecycle.CountObservers[observerqueries.Record](set)
	plains := lifecycle.CountObservers[observerqueries.Plain](set)
	effects := lifecycle.CountObservers[observerqueries.Effect](set)
	if records.Write != 2 || plains.Write != 1 || effects.Write != 0 {
		return errors.New("forwarded row stream lost the database's typed observer registrations")
	}
	if records.Retrieval != 1 || effects.Retrieval != 1 {
		return errors.New("forwarded row stream lost its separate retrieval registrations")
	}

	return nil
}

func TestPostgresReadsRetainObserversWithoutConstructingWriteHooks(t *testing.T) {
	db, stats, namespace := observerApp(t)
	var streams []*database.Rows
	read := func(tx *database.Tx) error {
		items, err := observerqueries.Records.Query().All(t.Context(), forwardingReader{tx, &streams})
		if err != nil {
			return err
		}
		if len(items) != 0 {
			return errors.New("new isolated fixture unexpectedly contained records")
		}
		return nil
	}
	if err := inSchema(t, db, namespace, func(tx *database.Tx) error {
		if err := read(tx); err != nil {
			return err
		}
		return tx.Savepoint(t.Context(), read)
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Session(t.Context(), func(session *database.Session) error {
		return inSchema(t, session, namespace, read)
	}); err != nil {
		t.Fatal(err)
	}
	if len(streams) != 3 {
		t.Fatal("expected one forwarded stream per model query")
	}
	for _, rows := range streams {
		if err := checkReadObservers(rows); err != nil {
			t.Fatal("ownership changed after transaction scope expiry", err)
		}
		if _, err := rows.Columns(); !errors.Is(err, database.Closed) {
			t.Fatal("model collection retained its row stream", err)
		}
	}
	if stats.First.Load() != 0 || stats.Second.Load() != 0 || stats.Plain.Load() != 0 || stats.Retrieval.Load() != 0 {
		t.Fatal("read metadata inspection constructed hook factories")
	}
}
