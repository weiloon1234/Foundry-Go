package translations

import (
	"context"
	"database/sql/driver"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/i18n"
	store "github.com/weiloon1234/Foundry-Go/internal/extensionstore"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/model"
)

// A batch of owners × locales beyond one keyset page loads completely in the
// same snapshot with one extra query per page instead of failing at 4096 rows.
func TestPostgresTranslationLoadPagesBeyondOnePage(t *testing.T) {
	fixture := extensiontest.Open(t, Migrations())
	const owners = 830
	if err := fixture.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO extension_members (id) SELECT generate_series(21,$1::bigint)`, owners)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	locales, err := i18n.NewLocaleSet("en", "en", "ms", "zh", "ta", "fr")
	if err != nil {
		t.Fatal(err)
	}
	name := Define(extensiontest.Members, "name", Options{})
	m, err := New(fixture.Store, locales, name.Registration())
	if err != nil {
		t.Fatal(err)
	}
	references := make([]model.Reference[extensiontest.Member, int64], owners)
	for i := range references {
		references[i] = extensiontest.Members.Reference(int64(i + 1))
		assignments := make([]Assignment[extensiontest.Member, int64], 0, 5)
		for _, locale := range locales.Locales() {
			assignments = append(assignments, name.SetValue(locale, string(locale)))
		}
		if err := Set(t.Context(), m, references[i], assignments...); err != nil {
			t.Fatal(err)
		}
	}
	fixture.Queries.Store(0)
	batch, err := name.Load(t.Context(), m, references)
	if err != nil {
		t.Fatal(err)
	}
	// One owner SELECT plus two keyset pages for 4150 rows.
	if got := fixture.Queries.Load(); got != 3 {
		t.Fatal("unexpected query count", got)
	}
	for _, reference := range []model.Reference[extensiontest.Member, int64]{references[0], references[owners/2], references[owners-1]} {
		values, err := batch.Get(reference)
		if err != nil || len(values.Entries()) != 5 {
			t.Fatal("paged batch lost rows", err, len(values.Entries()))
		}
	}
}

// statementConnector counts every statement sent to PostgreSQL, including
// INSERT/DELETE, while forwarding the optional pgx driver interfaces.
type statementConnector struct {
	driver.Connector
	statements *atomic.Int64
}
type statementConnection struct {
	driver.Conn
	statements *atomic.Int64
}

func (c statementConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &statementConnection{Conn: conn, statements: c.statements}, nil
}
func (c *statementConnection) QueryContext(ctx context.Context, sql string, args []driver.NamedValue) (driver.Rows, error) {
	c.statements.Add(1)
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, sql, args)
}
func (c *statementConnection) ExecContext(ctx context.Context, sql string, args []driver.NamedValue) (driver.Result, error) {
	c.statements.Add(1)
	return c.Conn.(driver.ExecerContext).ExecContext(ctx, sql, args)
}
func (c *statementConnection) BeginTx(ctx context.Context, options driver.TxOptions) (driver.Tx, error) {
	return c.Conn.(driver.ConnBeginTx).BeginTx(ctx, options)
}
func (c *statementConnection) Ping(ctx context.Context) error {
	return c.Conn.(driver.Pinger).Ping(ctx)
}
func (c *statementConnection) ResetSession(ctx context.Context) error {
	if r, ok := c.Conn.(driver.SessionResetter); ok {
		return r.ResetSession(ctx)
	}
	return nil
}
func (c *statementConnection) IsValid() bool {
	if r, ok := c.Conn.(driver.Validator); ok {
		return r.IsValid()
	}
	return true
}
func (c *statementConnection) CheckNamedValue(v *driver.NamedValue) error {
	if r, ok := c.Conn.(driver.NamedValueChecker); ok {
		return r.CheckNamedValue(v)
	}
	return driver.ErrSkip
}

func TestPostgresTranslationSetBasedWritesAndRegionalFallback(t *testing.T) {
	var sent atomic.Int64
	fixture := extensiontest.OpenWithConnector(t, Migrations(), func(c driver.Connector) driver.Connector {
		return statementConnector{Connector: c, statements: &sent}
	})
	locales, err := i18n.NewLocaleSet("ms", "ms", "en", "en-GB")
	if err != nil {
		t.Fatal(err)
	}
	owner := extensiontest.Members
	title := Define(owner, "title", Options{})
	fields := make([]Field[extensiontest.Member, int64], 12)
	registrations := []Registration{title.Registration()}
	for i := range fields {
		fields[i] = Define(owner, Name("field."+strings.Repeat("x", i+1)), Options{})
		registrations = append(registrations, fields[i].Registration())
	}
	m, err := New(fixture.Store, locales, registrations...)
	if err != nil {
		t.Fatal(err)
	}
	statements := func(assignments ...Assignment[extensiontest.Member, int64]) int64 {
		sent.Store(0)
		if err := Set(t.Context(), m, owner.Reference(2), assignments...); err != nil {
			t.Fatal(err)
		}
		return sent.Load()
	}
	small := statements(fields[0].SetValue("en", "one"))
	var many []Assignment[extensiontest.Member, int64]
	for _, field := range fields {
		for _, locale := range locales.Locales() {
			many = append(many, field.SetValue(locale, "text"))
		}
	}
	if large := statements(many...); large != small {
		t.Fatal("writes are not set-based", small, large)
	}
	sent.Store(0)
	removed, err := fields[1].Clear(t.Context(), m, owner.Reference(2))
	if err != nil || removed != 3 {
		t.Fatal("clear", removed, err)
	}
	cleared := sent.Load()
	sent.Store(0)
	removed, err = DeleteAll(t.Context(), m, owner, owner.Reference(2))
	if err != nil || removed != len(many)-3 {
		t.Fatal("delete all", removed, err)
	}
	if deleted := sent.Load(); deleted != cleared {
		t.Fatal("deletes are not set-based", cleared, deleted)
	}

	if err := Set(t.Context(), m, owner.Reference(1), title.SetValue("en", "Color"), title.SetValue("ms", "Warna")); err != nil {
		t.Fatal(err)
	}
	resolved, err := title.Resolve(t.Context(), m, owner.Reference(1), "en-GB")
	got, ok := resolved.Get()
	if err != nil || !ok || got.Locale != "en" || got.Text != "Color" {
		t.Fatal("regional parent fallback", got, err)
	}
}

func TestPostgresTranslationMatchingUsesValueHashIndex(t *testing.T) {
	fixture := extensiontest.Open(t, Migrations())
	owner := extensiontest.Members
	name := Define(owner, "name", Options{})
	m, err := New(fixture.Store, testLocales(t), name.Registration())
	if err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("長い値", 4000)
	for id, text := range map[int64]string{1: "Red", 2: "Red", 3: "Blue", 4: long} {
		if err := name.Set(t.Context(), m, owner.Reference(id), "en", text); err != nil {
			t.Fatal(err)
		}
	}
	for text, want := range map[string]int64{"Red": 2, "Blue": 1, long: 1, "Green": 0} {
		predicate, err := name.Matching(t.Context(), m, "en", text)
		if err != nil {
			t.Fatal(err)
		}
		var count int64
		if err := fixture.Store.Read(t.Context(), func(ctx context.Context, tx *database.Tx) error {
			count, err = extensiontest.MemberQuery().Where(predicate).Count(ctx, tx)
			return err
		}); err != nil || count != want {
			t.Fatal("matching", want, count, err)
		}
	}
	if err := fixture.Store.Read(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL enable_seqscan = off`); err != nil {
			return err
		}
		f := store.TranslationFields()
		plan, err := store.QueryFoundryModelTranslations().Where(f.Value.Eq("Red")).Explain(ctx, tx)
		if err != nil {
			return err
		}
		if !strings.Contains(string(plan.JSON()), "foundry_model_translations_value") {
			t.Error("exact value lookup does not use the value hash index")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
