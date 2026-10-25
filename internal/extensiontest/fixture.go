// Package extensiontest supplies isolated PostgreSQL model-store acceptance
// fixtures. No fixture drops schemas, resets data or starts a database server.
package extensiontest

import (
	"context"
	"database/sql/driver"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/temporal"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Member struct {
	ID        int64
	DeletedAt value.Nullable[temporal.DateTime]
}
type Other struct {
	ID        int64
	DeletedAt value.Nullable[temporal.DateTime]
}
type Product struct{ SKU string }

// Identity reads must never hydrate presentation models or invoke retrieval hooks.
func noPresentation[M any]() orm.Option {
	return orm.WithRetrievalHooks(func() orm.RetrievalHooks[M] {
		panic("extension ownership must not hydrate model presentation")
	})
}

var products = orm.Define("extension_products", func(b orm.Builder[Product], m *Product) orm.Table[Product, string] {
	return orm.TableOf(b, &m.SKU)
}, noPresentation[Product]())

var Products = extensions.DefineORMOwner("products", orm.IdentityOf(products, products.Key()), extensions.OwnerOptions{})

type memberTable struct {
	orm.Table[Member, int64]
	DeletedAt orm.SoftDelete[Member, temporal.DateTime]
}

func memberSource(table string) memberTable {
	return orm.Define(table, func(b orm.Builder[Member], m *Member) memberTable {
		return memberTable{orm.TableOf(b, &m.ID), orm.SoftDeleteOf(b, &m.DeletedAt)}
	}, noPresentation[Member]())
}

var memberFields = memberSource("extension_members")

func MemberQuery() orm.Query[Member]        { return memberFields.Query() }
func MemberIDField() orm.Key[Member, int64] { return memberFields.Key() }

var Members = extensions.DefineORMOwner("members", orm.IdentityOf(memberFields.Table, memberFields.Key()), extensions.OwnerOptions{})

// MemberIdentity describes the member fixture model stored in table. Rename
// scenarios declare the same owner over a renamed table with it.
func MemberIdentity(table string) orm.ModelIdentity[Member, int64] {
	fields := memberSource(table)
	return orm.IdentityOf(fields.Table, fields.Key())
}

type otherTable struct {
	orm.Table[Other, int64]
	DeletedAt orm.SoftDelete[Other, temporal.DateTime]
}

var otherFields = orm.Define("extension_others", func(b orm.Builder[Other], m *Other) otherTable {
	return otherTable{orm.TableOf(b, &m.ID), orm.SoftDeleteOf(b, &m.DeletedAt)}
}, noPresentation[Other]())
var Others = extensions.DefineORMOwner("others", orm.IdentityOf(otherFields.Table, otherFields.Key()), extensions.OwnerOptions{})

type Fixture struct {
	DB      *database.DB
	Store   *extensions.Store
	Schema  string
	Queries *atomic.Int64
}

func Open(t testing.TB, definitions []migrate.Definition) Fixture {
	t.Helper()
	return OpenWithConnector(t, definitions, nil)
}

// OpenWithConnector permits a test to inject failures at the real driver boundary.
// The wrapper must preserve optional driver interfaces and own no credentials.
func OpenWithConnector(t testing.TB, definitions []migrate.Definition, wrap func(driver.Connector) driver.Connector) Fixture {
	t.Helper()
	config := pgtest.Config(t)
	adapter, err := postgres.New(config)
	if err != nil {
		t.Fatal(err)
	}
	queries := &atomic.Int64{}
	if wrap != nil {
		adapter.Connector = wrap(adapter.Connector)
	}
	adapter.Connector = countConnector{Connector: adapter.Connector, queries: queries}
	db, err := database.Open(t.Context(), adapter, config.Pool)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := db.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	schema := pgtest.Namespace(t, db)
	owners, err := extensions.NewRegistry(Members.Registration(), Others.Registration(), Products.Registration())
	if err != nil {
		t.Fatal(err)
	}
	settings := extensions.DefaultConfig()
	settings.Schema = schema
	store, err := extensions.New(db, owners, settings)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := store.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	f := Fixture{DB: db, Store: store, Schema: schema, Queries: queries}
	if err := store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		for _, table := range []string{"extension_members", "extension_others"} {
			if _, err := tx.Exec(ctx, `CREATE TABLE `+table+` (id bigint PRIMARY KEY,deleted_at timestamptz)`); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO `+table+` (id) SELECT generate_series(1,20)`); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `CREATE TABLE extension_products (sku text PRIMARY KEY)`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO extension_products (sku) VALUES ('shirt:蓝/L'),('another')`); err != nil {
			return err
		}
		for _, definition := range definitions {
			for _, sql := range definition.SQL {
				if _, err := tx.Exec(ctx, sql); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return f
}

// The underlying adapter is pgx's standard-library connector. Forward its
// optional context/lifetime interfaces while counting actual SELECT executions.
type countConnector struct {
	driver.Connector
	queries *atomic.Int64
}

func (c countConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &countConnection{Conn: conn, queries: c.queries}, nil
}

type countConnection struct {
	driver.Conn
	queries *atomic.Int64
}

func (c *countConnection) QueryContext(ctx context.Context, sql string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(sql)), "SELECT") {
		c.queries.Add(1)
	}
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, sql, args)
}
func (c *countConnection) ExecContext(ctx context.Context, sql string, args []driver.NamedValue) (driver.Result, error) {
	return c.Conn.(driver.ExecerContext).ExecContext(ctx, sql, args)
}
func (c *countConnection) BeginTx(ctx context.Context, options driver.TxOptions) (driver.Tx, error) {
	return c.Conn.(driver.ConnBeginTx).BeginTx(ctx, options)
}
func (c *countConnection) Ping(ctx context.Context) error { return c.Conn.(driver.Pinger).Ping(ctx) }
func (c *countConnection) ResetSession(ctx context.Context) error {
	if r, ok := c.Conn.(driver.SessionResetter); ok {
		return r.ResetSession(ctx)
	}
	return nil
}
func (c *countConnection) IsValid() bool {
	if r, ok := c.Conn.(driver.Validator); ok {
		return r.IsValid()
	}
	return true
}
func (c *countConnection) CheckNamedValue(v *driver.NamedValue) error {
	if r, ok := c.Conn.(driver.NamedValueChecker); ok {
		return r.CheckNamedValue(v)
	}
	return driver.ErrSkip
}
