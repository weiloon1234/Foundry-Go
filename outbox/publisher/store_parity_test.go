package publisher_test

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/outboxstore"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/outbox"
	"github.com/weiloon1234/Foundry-Go/outbox/publisher"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

const parityID = "01890f47-1a00-7000-8000-000000000001"

var parityTime = time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)

type storeStatement struct {
	SQL  string
	Args []any
}
type storeCapture struct {
	Statements                 []storeStatement
	Begins, Commits, Rollbacks int
	mode                       string
	queries                    int
}
type storeConnector struct{ capture *storeCapture }

func (c storeConnector) Driver() driver.Driver { return c }
func (c storeConnector) Connect(context.Context) (driver.Conn, error) {
	return &storeConnection{c.capture}, nil
}
func (c storeConnector) Open(string) (driver.Conn, error) { return c.Connect(context.Background()) }

type storeConnection struct{ *storeCapture }

func (*storeConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*storeConnection) Close() error { return nil }
func (c *storeConnection) Begin() (driver.Tx, error) {
	c.Begins++
	return storeTransaction{c.storeCapture}, nil
}
func (c *storeConnection) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return c.Begin()
}
func (*storeConnection) Ping(context.Context) error { return nil }

type storeTransaction struct{ capture *storeCapture }

func (tx storeTransaction) Commit() error {
	tx.capture.Commits++
	if tx.capture.mode == "unknown" {
		return errors.New("commit response lost")
	}
	return nil
}
func (tx storeTransaction) Rollback() error { tx.capture.Rollbacks++; return nil }
func (c *storeCapture) record(sql string, args []driver.NamedValue) {
	values := make([]any, len(args))
	for i, a := range args {
		values[i] = a.Value
	}
	if strings.HasPrefix(sql, "INSERT") {
		id, err := model.ParseID[outboxstore.Message](values[0].(string))
		if err != nil || id.IsZero() || id.Bytes()[6]>>4 != 7 {
			panic("expected generated UUIDv7")
		}
		values[0] = "<generated UUIDv7>"
	}
	c.Statements = append(c.Statements, storeStatement{sql, values})
}
func (c *storeConnection) ExecContext(_ context.Context, sql string, args []driver.NamedValue) (driver.Result, error) {
	c.record(sql, args)
	if strings.HasPrefix(sql, "DELETE") {
		c.queries++
	}
	return driver.RowsAffected(1), nil
}
func (c *storeConnection) QueryContext(_ context.Context, sql string, args []driver.NamedValue) (driver.Rows, error) {
	c.record(sql, args)
	c.queries++
	if strings.HasPrefix(sql, "SELECT COUNT") {
		return &storeRows{values: []driver.Value{int64(1)}}, nil
	}
	state := string(outbox.Pending)
	if c.mode == "requeue" || c.mode == "failed" {
		state = string(outbox.PublicationFailed)
	}
	if strings.HasPrefix(sql, "UPDATE") {
		state = args[0].Value.(string)
	}
	origin, err := json.Marshal(attribution.Origin{})
	if err != nil {
		return nil, err
	}
	return &storeRows{values: []driver.Value{parityID, "event", "events", "notice", int64(1), `{"n":1}`, string(origin), parityTime, state, int64(0), parityTime, nil, ""}}, nil
}

type storeRows struct {
	values []driver.Value
	done   bool
}

func (r *storeRows) Columns() []string { return make([]string, len(r.values)) }
func (*storeRows) Close() error        { return nil }
func (r *storeRows) Next(dst []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	copy(dst, r.values)
	return nil
}

// The frozen transcript was captured by this same test against 5cbe31d's generated
// store. Only generated UUID entropy is normalized; SQL, bindings and savepoints
// must remain exact. This driver never connects to PostgreSQL.
func TestOutboxStoreParity(t *testing.T) {
	for _, mode := range []string{"append", "find", "publish", "retry", "exhausted", "unknown", "requeue", "prune", "failed", "stats"} {
		t.Run(mode, func(t *testing.T) {
			capture := &storeCapture{mode: mode}
			source := testkit.NewClock(parityTime)
			db, err := database.Open(t.Context(), database.Adapter{Connector: storeConnector{capture}}, database.DefaultPoolConfig(), database.WithClock(source))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := db.Close(context.Background()); err != nil {
					t.Error(err)
				}
			})
			address := outboxstore.Address{Kind: "event", Destination: "events", Name: "notice", Version: 1}
			calls := 0
			config := publisher.DefaultConfig()
			config.Clock = source
			config.Jitter = 0
			if mode == "exhausted" {
				config.MaxAttempts = 1
			}
			p, err := publisher.New(db, config, publisher.Route{Kind: address.Kind, Destination: address.Destination, Publish: func(_ context.Context, message publisher.Message) error {
				calls++
				if mode == "retry" || mode == "exhausted" {
					return errors.New("transient publication failure")
				}
				return nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			want := 1
			switch mode {
			case "append":
				err = db.Transaction(t.Context(), func(tx *database.Tx) error {
					row, e := outboxstore.Append(t.Context(), tx, address, `{"n":1}`, attribution.Origin{})
					payload, payloadErr := row.Payload.Text()
					if e == nil && (payloadErr != nil || row.ID.String() != parityID || row.PublishState != outbox.Pending || payload != `{"n":1}` || !row.PublishedAt.IsNull()) {
						t.Fatal("stored defaults or hydration changed")
					}
					return e
				})
			case "find":
				id, e := model.ParseID[outboxstore.Message](parityID)
				if e != nil {
					t.Fatal(e)
				}
				row, e := outboxstore.Find(t.Context(), db, address, id)
				err = e
				if e == nil && !row.IsSet() {
					t.Fatal("missing stored row")
				}
			case "publish", "retry", "exhausted", "unknown":
				want = 2
				result, e := p.PublishOne(t.Context())
				err = e
				if mode == "unknown" {
					var outcome *database.Error
					if !errors.As(e, &outcome) || outcome.Outcome() != database.Unknown || result.Committed || !result.Found || calls != 1 {
						t.Fatal("unknown commit lost reconciliation outcome", e)
					}
					err = nil
				} else {
					state := outbox.Published
					if mode == "retry" {
						state = outbox.Pending
					}
					if mode == "exhausted" {
						state = outbox.PublicationFailed
					}
					if e == nil && (!result.Committed || !result.Found || result.State != state || result.Attempts != 1 || calls != 1) {
						t.Fatal("publication outcome changed")
					}
				}
			case "requeue":
				want = 2
				n, e := p.Requeue(t.Context(), publisher.Selection{Kind: "event", Limit: 2})
				err = e
				if e == nil && n != 1 {
					t.Fatal("requeue count", n)
				}
			case "prune":
				n, e := p.Prune(t.Context(), parityTime, 2)
				err = e
				if e == nil && n != 1 {
					t.Fatal("prune count", n)
				}
			case "failed":
				rows, e := p.Failed(t.Context(), publisher.Selection{Kind: "event", Limit: 2})
				err = e
				if e == nil && len(rows) != 1 {
					t.Fatal("failure listing", len(rows))
				}
			case "stats":
				want = 3
				stats, e := p.Stats(t.Context())
				err = e
				if e == nil && (stats.Pending != 1 || stats.Published != 1 || stats.Failed != 1) {
					t.Fatal("state counts", stats)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if capture.queries != want {
				t.Fatalf("statements: got %d want %d", capture.queries, want)
			}
			data, err := json.MarshalIndent(capture, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join("testdata", "store_"+mode+".json")
			expected, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var a, b any
			if json.Unmarshal(expected, &a) != nil || json.Unmarshal(data, &b) != nil || !reflect.DeepEqual(a, b) {
				t.Fatalf("old/new outbox transcript differs:\n%s", data)
			}
		})
	}
}
