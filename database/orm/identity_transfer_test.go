package orm

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
	"reflect"
	"strings"
	"testing"
)

type identityTransferRecord struct {
	ID        int64
	Name      string
	DeletedAt value.Nullable[temporal.DateTime]
}
type identityTransferTable struct {
	Table[identityTransferRecord, int64]
	Name      Text[identityTransferRecord, string]
	DeletedAt SoftDelete[identityTransferRecord, temporal.DateTime]
}

func identityTransferFields() identityTransferTable {
	return Define("identity_records", func(b Builder[identityTransferRecord], m *identityTransferRecord) identityTransferTable {
		return identityTransferTable{TableOf(b, &m.ID), TextOf(b, &m.Name), SoftDeleteOf(b, &m.DeletedAt)}
	})
}
func TestIdentityDescriptorRejectsNonPrimaryAndScopedQueries(t *testing.T) {
	d := identityTransferFields()
	identity := IdentityOf(d.Table, d.Key())
	if e := identity.Validate(); e != nil {
		t.Fatal(e)
	}
	nonprimary := identity
	nonprimary.key.core = d.Name.text.core
	for _, bad := range []ModelIdentity[identityTransferRecord, int64]{{}, IdentityOf(d.Table, identityTransferFields().Key()), IdentityOf(d.Table, Key[identityTransferRecord, int64]{}), nonprimary} {
		if e := bad.Validate(); !errors.Is(e, fault.Invalid) {
			t.Fatal("nonprimary or foreign identity", e)
		}
	}
	id, e := identity.Reference(9).Identity()
	if e != nil {
		t.Fatal(e)
	}
	parsed, e := identity.Parse(id)
	if e != nil || parsed.Key() != 9 {
		t.Fatal(e)
	}
	foreign, e := model.NewReference[identityTransferRecord]("another", int64(9), codec.Signed[int64]()).Identity()
	if e != nil {
		t.Fatal(e)
	}
	if _, e := identity.Parse(foreign); e == nil {
		t.Fatal("foreign namespace")
	}
	ex := &uniqueTransferExecutor{cause: errors.New("capture SQL")}
	_, _ = identityKeys(t.Context(), ex, identity, []int64{1}, false, true)
	if !strings.Contains(ex.sql, `"deleted_at" IS NULL`) || !strings.Contains(ex.sql, "FOR UPDATE") || strings.Contains(ex.sql, `"name"`) {
		t.Fatal("identity lost key-only visibility lock", ex.sql)
	}
	// IdentityOf accepts a Table, not a Query: scoped/limited inputs cannot enter
	// this API. Its runtime validation still rejects forged non-primary metadata.
}
func TestPostgresIdentityBatchVisibilityAndPrimaryOnlyLock(t *testing.T) {
	db := pgtest.Isolate(t).Open(t)
	d := identityTransferFields()
	for _, sql := range []string{`CREATE TABLE identity_records(id bigint PRIMARY KEY,name text NOT NULL,deleted_at timestamptz)`, `INSERT INTO identity_records VALUES(1,'active',NULL),(2,'hidden',CURRENT_TIMESTAMP)`} {
		if _, e := db.Exec(t.Context(), sql); e != nil {
			t.Fatal(e)
		}
	}
	identity := IdentityOf(d.Table, d.Key())
	e := db.Transaction(t.Context(), func(tx *database.Tx) error {
		active, e := identity.ActiveKeys(t.Context(), tx, []int64{1, 2, 3, 1})
		if e != nil {
			return e
		}
		if !reflect.DeepEqual(active, []int64{1}) {
			t.Fatal(active)
		}
		retained, e := identity.RetainedKeys(t.Context(), tx, []int64{1, 2, 3})
		if e != nil {
			return e
		}
		if !reflect.DeepEqual(retained, []int64{1, 2}) {
			t.Fatal(retained)
		}
		for _, key := range []int64{1, 2, 3} {
			found, e := identity.LockActive(t.Context(), tx, key)
			if e != nil {
				return e
			}
			if found != (key == 1) {
				t.Fatal(key, found)
			}
		}
		if _, e := identity.ActiveKeys(t.Context(), tx, make([]int64, querycore.MaxIdentityBatch+1)); e == nil {
			t.Fatal("oversized identity batch")
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
}
func TestPostgresInsertOrFirstResolvesConcurrentCreation(t *testing.T) {
	d := scopeTransferFields()
	db := pgtest.Isolate(t).Open(t)
	if _, e := db.Exec(t.Context(), `CREATE TABLE scope_records(id bigint PRIMARY KEY,rank bigint NOT NULL UNIQUE)`); e != nil {
		t.Fatal(e)
	}
	q := d.Query().Where(d.Rank.Eq(7))
	type outcome struct {
		row scopeTransferRow
		err error
	}
	out := make(chan outcome, 4)
	for i := range 4 {
		go func() {
			r, e := CreateOrFirst(t.Context(), db, q, d.Key().Set(int64(i+1)), d.Rank.Set(7))
			out <- outcome{r, e}
		}()
	}
	var winner int64
	for range 4 {
		v := <-out
		if v.err != nil {
			t.Fatal(v.err)
		}
		if winner == 0 {
			winner = v.row.ID
		}
		if winner == 0 || v.row.ID != winner || v.row.Rank != 7 {
			t.Fatal("different winners", v.row, winner)
		}
	}
	if _, e := CreateOrFirst(t.Context(), db, d.Query().Where(d.Rank.Eq(8)), d.Key().Set(winner), d.Rank.Set(8)); !errors.Is(e, database.UniqueViolation) {
		t.Fatal("unselected conflict hidden", e)
	}
	if e := db.Transaction(t.Context(), func(tx *database.Tx) error {
		r, e := CreateOrFirst(t.Context(), tx, q, d.Key().Set(99), d.Rank.Set(7))
		if e != nil {
			return e
		}
		if r.ID != winner {
			t.Fatal(r, winner)
		}
		_, e = tx.Exec(t.Context(), "SELECT 1")
		return e
	}); e != nil {
		t.Fatal(e)
	}
}
