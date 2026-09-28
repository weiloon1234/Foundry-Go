package query

import (
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

type identityRecord struct {
	ID        int64
	Name      string
	DeletedAt value.Nullable[temporal.DateTime]
}

func identityQuery() Query[identityRecord] {
	return ForModel(Define[identityRecord]("identity_records", "id", []Column{{Name: "id"}, {Name: "name"}, {Name: "deleted_at", Nullable: true}}, func(database.Row) (identityRecord, error) { panic("identity lookup must not hydrate models") },
		NewModelField("id", codec.Signed[int64](), func(m identityRecord) int64 { return m.ID }),
		NewModelField("name", codec.String[string](), func(m identityRecord) string { return m.Name }),
		NewModelField("deleted_at", codec.Nullable(codec.DateTime()), func(m identityRecord) value.Nullable[temporal.DateTime] { return m.DeletedAt }),
	).WithSoftDeletes("deleted_at"))
}
func TestIdentityDescriptorRejectsNonPrimaryAndScopedQueries(t *testing.T) {
	q := identityQuery()
	primary := NewExactField[identityRecord]("identity_records", "id", codec.Signed[int64]())
	d := IdentityOf(q, primary)
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []ModelIdentity[identityRecord, int64]{IdentityOf(q.Limit(1), primary), IdentityOf(q.WithTrashed(), primary), IdentityOf(q, NewExactField[identityRecord]("different", "id", codec.Signed[int64]())), IdentityOf(q, NewExactField[identityRecord]("identity_records", "missing", codec.Signed[int64]()))} {
		if bad.Validate() == nil {
			t.Fatal("invalid identity descriptor")
		}
	}
	if IdentityOf(q, NewTextField[identityRecord]("identity_records", "name", codec.String[string]())).Validate() == nil {
		t.Fatal("non-primary identity accepted")
	}
	id, err := d.Reference(9).Identity()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := d.Parse(id)
	if err != nil || parsed.Key() != 9 {
		t.Fatal("key codec not reused", err)
	}
	other, err := model.NewReference[identityRecord]("another", int64(9), codec.Signed[int64]()).Identity()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Parse(other); err == nil {
		t.Fatal("different model namespace accepted")
	}
	statement, err := d.reader([]int64{1}, false, true).Compile()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(statement.SQL(), `"deleted_at" IS NULL`) || !strings.Contains(statement.SQL(), "FOR UPDATE") || strings.Contains(statement.SQL(), `"name"`) {
		t.Fatal("identity lookup did not preserve key-only lock/visibility semantics", statement.SQL())
	}
}
func TestPostgresIdentityBatchVisibilityAndPrimaryOnlyLock(t *testing.T) {
	db := pgtest.Open(t)
	schema := pgtest.Namespace(t, db)
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		if _, err := tx.Exec(t.Context(), `SET LOCAL search_path TO "`+schema+`"`); err != nil {
			return err
		}
		if _, err := tx.Exec(t.Context(), `CREATE TABLE identity_records(id bigint PRIMARY KEY,name text NOT NULL,deleted_at timestamptz)`); err != nil {
			return err
		}
		if _, err := tx.Exec(t.Context(), `INSERT INTO identity_records VALUES (1,'active',NULL),(2,'hidden',CURRENT_TIMESTAMP)`); err != nil {
			return err
		}
		d := IdentityOf(identityQuery(), NewExactField[identityRecord]("identity_records", "id", codec.Signed[int64]()))
		active, err := d.ActiveKeys(t.Context(), tx, []int64{1, 2, 3, 1})
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(active, []int64{1}) {
			t.Fatal("active visibility", active)
		}
		retained, err := d.RetainedKeys(t.Context(), tx, []int64{1, 2, 3})
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(retained, []int64{1, 2}) {
			t.Fatal("orphan inspection visibility", retained)
		}
		for _, key := range []int64{1, 2, 3} {
			ok, err := d.LockActive(t.Context(), tx, key)
			if err != nil {
				return err
			}
			if ok != (key == 1) {
				t.Fatal("lock visibility", key)
			}
		}
		if _, err := d.ActiveKeys(t.Context(), tx, make([]int64, MaxIdentityBatch+1)); err == nil {
			t.Fatal("oversized identity batch accepted")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
