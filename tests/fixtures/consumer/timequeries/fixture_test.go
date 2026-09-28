package timequeries_test

import (
	"errors"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	foundry "github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/testkit"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

type countingClock struct {
	source     *testkit.Clock
	calls      atomic.Int32
	nextAction atomic.Int32
}

func (c *countingClock) Now() time.Time {
	c.calls.Add(1)
	switch c.nextAction.Swap(0) {
	case 1:
		panic("private clock fixture payload")
	case 2:
		runtime.Goexit()
	}
	return c.source.Now()
}
func (c *countingClock) stored() time.Time { return c.source.Now().UTC().Truncate(time.Microsecond) }

func runTimed(t *testing.T, work func(*database.Tx, *countingClock) error) {
	t.Helper()
	clock := &countingClock{source: testkit.NewClock(time.Date(2030, 1, 2, 3, 4, 5, 123456789, time.UTC))}
	pool := foundation.NewKey[*database.DB]("time.pool")
	app := testkit.Start(t, foundry.New(foundation.WithClock(clock)).Register(postgres.Module("database", pool, pgtest.Config(t))))
	db, err := foundation.Resolve(app.Services(), pool)
	if err != nil {
		t.Fatal(err)
	}
	namespace := pgtest.Namespace(t, db)
	err = db.Transaction(t.Context(), func(tx *database.Tx) error {
		if tx.Clock() != clock {
			return errors.New("transaction did not retain the application's clock")
		}
		for _, ddl := range []string{
			`SET LOCAL search_path TO "` + namespace + `"`,
			`CREATE TABLE time_members(id uuid PRIMARY KEY,name text NOT NULL,created_on timestamptz NOT NULL,changed_on timestamptz NOT NULL)`,
			`CREATE TABLE time_manual(id uuid PRIMARY KEY,created_at timestamptz NOT NULL,updated_at timestamptz NOT NULL)`,
			`CREATE FUNCTION adjust_time() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN NEW.changed_on := NEW.changed_on + INTERVAL '2 seconds'; RETURN NEW; END $$`,
			`CREATE TRIGGER adjust_time BEFORE UPDATE ON time_members FOR EACH ROW EXECUTE FUNCTION adjust_time()`,
		} {
			if _, err := tx.Exec(t.Context(), ddl); err != nil {
				return err
			}
		}
		clock.calls.Store(0)
		return work(tx, clock)
	})
	if err != nil {
		t.Fatal(err)
	}
}
