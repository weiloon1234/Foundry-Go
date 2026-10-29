package orm

import "time"

import (
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/encrypted"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestEncryptedOpenFailurePublication(t *testing.T) {
	r := secureRecords()
	keys := sensitiveKeys(t)
	id, err := model.NewIDAt[secureRecord](time.Now())
	if err != nil {
		t.Fatal(err)
	}
	other, err := model.NewIDAt[secureRecord](time.Now())
	if err != nil {
		t.Fatal(err)
	}
	binding, err := encrypted.Binding("secure_records", "secret", id.String())
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := encrypted.NewText("private").Seal(t.Context(), keys, binding)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := encrypted.TextCodec().Bind(sealed)
	if err != nil {
		t.Fatal(err)
	}
	good := []driver.Value{id.String(), raw, nil}
	bad := []driver.Value{other.String(), raw, nil}
	ctx := t.Context()
	one := func(m secureRecord) int {
		if reflect.ValueOf(m).IsZero() {
			return 0
		}
		return 1
	}
	cases := []struct {
		name   string
		single bool
		want   int
		run    func(*database.DB) (int, error)
	}{
		{"Raw", false, 0, func(db *database.DB) (int, error) {
			v, e := Raw(ctx, db, r.Table, "SELECT id,secret,backup FROM secure_records")
			return len(v), e
		}},
		{"All", false, 0, func(db *database.DB) (int, error) { v, e := All(ctx, db, r.Query()); return len(v), e }},
		{"First", true, 0, func(db *database.DB) (int, error) {
			v, e := First(ctx, db, r.Query())
			_, ok := v.Get()
			if ok {
				return 1, e
			}
			return 0, e
		}},
		{"Require", true, 0, func(db *database.DB) (int, error) { v, e := Require(ctx, db, r.Query()); return one(v), e }},
		{"Find", true, 0, func(db *database.DB) (int, error) {
			v, e := Find(ctx, db, r.Table, other)
			_, ok := v.Get()
			if ok {
				return 1, e
			}
			return 0, e
		}},
		{"TableRequire", true, 0, func(db *database.DB) (int, error) { v, e := RequireKey(ctx, db, r.Table, other); return one(v), e }},
		{"Each", false, 1, func(db *database.DB) (int, error) {
			n := 0
			e := Each(ctx, db, r.Query(), func(v secureRecord) error {
				if v.ID != id {
					t.Fatal("yielded failing or subsequent row")
				}
				n++
				return nil
			})
			return n, e
		}},
		{"Chunk", false, 0, func(db *database.DB) (int, error) {
			n := 0
			e := Chunk(ctx, db, r.Query(), 10, func(v []secureRecord) error { n += len(v); return nil })
			return n, e
		}},
		{"EachChunked", false, 0, func(db *database.DB) (int, error) {
			n := 0
			e := EachChunked(ctx, db, r.Query(), 10, func(secureRecord) error { n++; return nil })
			return n, e
		}},
		{"Paginate", false, 0, func(db *database.DB) (int, error) {
			v, e := Paginate(ctx, db, r.Query(), Page(1, 10))
			return len(v.Items), e
		}},
		{"SimplePaginate", false, 0, func(db *database.DB) (int, error) {
			v, e := SimplePaginate(ctx, db, r.Query(), Page(1, 10))
			return len(v.Items), e
		}},
		{"Insert", true, 0, func(db *database.DB) (int, error) {
			v, e := Insert(ctx, db, r.Table, EncryptedSet(r.Secret, encrypted.NewText("assigned")))
			return one(v), e
		}},
		{"Update", true, 0, func(db *database.DB) (int, error) {
			v, e := Update(ctx, db, r.Table.Query(), r.Table.Key(), other, EncryptedSet(r.Secret, encrypted.NewText("assigned")))
			return one(v), e
		}},
		{"Delete", true, 0, func(db *database.DB) (int, error) {
			v, e := Delete(ctx, db, r.Table.Query(), r.Table.Key(), other)
			return one(v), e
		}},
		{"InsertMany", false, 0, func(db *database.DB) (int, error) {
			v, e := InsertMany(ctx, db, r.Table, [][]Assignment[secureRecord]{{EncryptedSet(r.Secret, encrypted.NewText("one"))}, {EncryptedSet(r.Secret, encrypted.NewText("two"))}, {EncryptedSet(r.Secret, encrypted.NewText("three"))}})
			return len(v), e
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var statements []string
			db := testDB(t, testConnector{statements: &statements, respond: func(sql string) ([]string, [][]driver.Value) {
				if strings.Contains(sql, "COUNT(") {
					return []string{"count"}, [][]driver.Value{{int64(3)}}
				}
				values := [][]driver.Value{good, bad, good}
				if tc.single {
					values = [][]driver.Value{bad}
				}
				return []string{"id", "secret", "backup"}, values
			}}, database.WithEncryption(keys))
			n, e := tc.run(db)
			if len(statements) == 0 {
				t.Fatal("did not reach hydration")
			}
			if !errors.Is(e, fault.Invalid) || n != tc.want {
				t.Fatalf("published=%d want=%d; expected authentication failure: %v", n, tc.want, e)
			}
		})
	}
}

type secureParent struct {
	ID       int64
	RecordID model.ID[secureRecord]
	Record   relation.One[secureRecord]
}
type secureParents struct {
	Table[secureParent, int64]
	RecordID Column[secureParent, model.ID[secureRecord]]
	Record   One[secureParent, secureRecord]
}

func TestEncryptedEagerOpenFailureDoesNotPublishParents(t *testing.T) {
	r := secureRecords()
	p := Define("secure_parents", func(b Builder[secureParent], p *secureParent) secureParents {
		return secureParents{TableOf(b, &p.ID), IDOf(b, &p.RecordID), OneOf(b, &p.Record)}
	})
	rel := BelongsToOf(p.Record, ColumnExpr(p.RecordID), KeyExpr(r.Key()))
	if err := Check(p, r, rel); err != nil {
		t.Fatal(err)
	}
	id, err := model.NewIDAt[secureRecord](time.Now())
	if err != nil {
		t.Fatal(err)
	}
	keys := sensitiveKeys(t)
	binding, err := encrypted.Binding("wrong_table", "secret", id.String())
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := encrypted.NewText("private").Seal(t.Context(), keys, binding)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := encrypted.TextCodec().Bind(sealed)
	if err != nil {
		t.Fatal(err)
	}
	db := testDB(t, testConnector{respond: func(sql string) ([]string, [][]driver.Value) {
		if strings.Contains(sql, `FROM "secure_parents"`) {
			return []string{"id", "record_id"}, [][]driver.Value{{int64(1), id.String()}}
		}
		return []string{"id", "secret", "backup"}, [][]driver.Value{{id.String(), raw, nil}}
	}}, database.WithEncryption(keys))
	got, err := All(t.Context(), db, p.Query().With(rel))
	if !errors.Is(err, fault.Invalid) || got != nil {
		t.Fatal("eager load published parents after open failure", err)
	}
}

type secureSoftRecord struct {
	ID        model.ID[secureSoftRecord]
	Secret    encrypted.Text
	DeletedAt value.Nullable[temporal.DateTime]
}
type secureSoftTable struct {
	Table[secureSoftRecord, model.ID[secureSoftRecord]]
	Secret Encrypted[secureSoftRecord, encrypted.Text]
	Soft   SoftDeletable[secureSoftRecord]
}

func TestEncryptedSoftWriteReturningFailure(t *testing.T) {
	r := Define("secure_soft_records", func(b Builder[secureSoftRecord], m *secureSoftRecord) secureSoftTable {
		deleted := SoftDeleteOf(b, &m.DeletedAt)
		return secureSoftTable{TableOf(b, &m.ID), EncryptedTextOf(b, &m.Secret), SoftDeletes(deleted)}
	})
	if err := Check(r); err != nil {
		t.Fatal(err)
	}
	keys := sensitiveKeys(t)
	id, err := model.NewIDAt[secureSoftRecord](time.Now())
	if err != nil {
		t.Fatal(err)
	}
	binding, err := encrypted.Binding("wrong_table", "secret", id.String())
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := encrypted.NewText("private").Seal(t.Context(), keys, binding)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := encrypted.TextCodec().Bind(sealed)
	if err != nil {
		t.Fatal(err)
	}
	for name, run := range map[string]func(*database.DB) (secureSoftRecord, error){
		"Restore": func(db *database.DB) (secureSoftRecord, error) { return Restore(t.Context(), db, r.Table, id) },
		"Delete": func(db *database.DB) (secureSoftRecord, error) {
			return Delete(t.Context(), db, r.Table.Query(), r.Table.Key(), id)
		},
		"ForceDelete": func(db *database.DB) (secureSoftRecord, error) {
			return ForceDelete(t.Context(), db, r.Table.Query(), r.Soft, r.Table.Key(), id)
		},
	} {
		t.Run(name, func(t *testing.T) {
			var statements []string
			db := testDB(t, testConnector{statements: &statements, columns: []string{"id", "secret", "deleted_at"}, values: [][]driver.Value{{id.String(), raw, nil}}}, database.WithEncryption(keys))
			got, err := run(db)
			if len(statements) == 0 {
				t.Fatal("did not reach RETURNING hydration")
			}
			if !errors.Is(err, fault.Invalid) || !reflect.ValueOf(got).IsZero() {
				t.Fatal("write published failed RETURNING model", err)
			}
		})
	}
}
