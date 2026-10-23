package pgsmoke_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"reflect"
	"strconv"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

type readRecord struct {
	ID   int64
	Rank int64
	Name value.Nullable[string]
}
type readTable struct {
	orm.Table[readRecord, int64]
	Rank orm.Number[readRecord, int64]
	Name orm.NullableText[readRecord, string]
}

func TestCursorChunksAndLocks(t *testing.T) {
	db := postgres.Isolate(t).Open(t)
	ctx := t.Context()
	_, err := db.Exec(ctx, `CREATE TABLE read_records (id bigint PRIMARY KEY,rank bigint NOT NULL,name text)`)
	must(t, err)
	_, err = db.Exec(ctx, `INSERT INTO read_records VALUES (1,2,'a'),(2,2,'a'),(3,1,'a'),(4,3,NULL),(5,3,NULL),(6,1,NULL)`)
	must(t, err)
	r := orm.Define("read_records", func(b orm.Builder[readRecord], m *readRecord) readTable {
		return readTable{orm.TableOf(b, &m.ID), orm.NumberOf(b, &m.Rank), orm.NullableTextOf(b, &m.Name)}
	})
	q := r.Query().OrderBy(r.Name.Asc(), r.Rank.Desc())
	request := query.CursorRequest[readRecord]{Size: 2}
	var pages []query.CursorPage[readRecord]
	var ids []int64
	for {
		page, err := orm.CursorPaginate(ctx, db, q, request)
		must(t, err)
		pages = append(pages, page)
		for _, m := range page.Items {
			ids = append(ids, m.ID)
		}
		if !page.Next.IsSet() {
			break
		}
		request.After = page.Next
		if len(pages) > 4 {
			t.Fatal("cursor did not progress")
		}
	}
	if !reflect.DeepEqual(ids, []int64{1, 2, 3, 4, 5, 6}) {
		t.Fatal("forward ties/NULLs", ids)
	}
	for i := len(pages) - 1; i > 0; i-- {
		page, err := orm.CursorPaginate(ctx, db, q, query.CursorRequest[readRecord]{Size: 2, Before: pages[i].Previous})
		must(t, err)
		if !reflect.DeepEqual(page.Items, pages[i-1].Items) {
			t.Fatal("backward ties/NULLs")
		}
	}
	foreign := q.Where(r.Rank.Eq(9))
	if _, err := orm.CursorPaginate(ctx, db, foreign, query.CursorRequest[readRecord]{Size: 2, After: pages[0].Next}); !errors.Is(err, fault.Invalid) {
		t.Fatal("foreign cursor accepted", err)
	}
	token, _ := pages[0].Next.Get()
	malformed, err := query.ParseCursor[readRecord](token.Token() + "!")
	if err == nil {
		_, err = orm.CursorPaginate(ctx, db, q, query.CursorRequest[readRecord]{Size: 2, After: value.Set(malformed)})
	}
	if !errors.Is(err, fault.Invalid) {
		t.Fatal("malformed token accepted", err)
	}
	var chunked []int64
	err = orm.ChunkByKey(ctx, db, r.Query(), r.Key(), 2, func(batch []readRecord) error {
		for _, m := range batch {
			chunked = append(chunked, m.ID)
		}
		if len(chunked) == 2 {
			// Ordinary deletes in this test's isolated table: no schema/reset operations.
			_, err := db.Exec(ctx, `DELETE FROM read_records WHERE id IN (1,3)`)
			return err
		}
		return nil
	})
	must(t, err)
	if !reflect.DeepEqual(chunked, []int64{1, 2, 4, 5, 6}) {
		t.Fatal("deletion shifted later chunks", chunked)
	}
	err = db.Transaction(ctx, func(tx *database.Tx) error {
		got, err := r.Query().Where(r.Key().Eq(2)).ForUpdate().Require(ctx, tx)
		if err == nil && got.ID != 2 {
			t.Fatal("locked lookup wrong row")
		}
		return err
	})
	must(t, err)
}

// A cursor is an unsigned position. Editing it never removes query predicates.
func TestEditedCursorPositionPreservesPredicates(t *testing.T) {
	db := postgres.Isolate(t).Open(t)
	ctx := t.Context()
	_, err := db.Exec(ctx, `CREATE TABLE position_records (id bigint PRIMARY KEY, tenant bigint NOT NULL, deleted_at timestamptz)`)
	must(t, err)
	_, err = db.Exec(ctx, `INSERT INTO position_records VALUES (1,7,NULL),(20,8,NULL),(21,8,NULL),(30,7,now()),(31,7,now()),(40,7,NULL),(50,7,NULL)`)
	must(t, err)
	type record struct {
		ID        int64
		Tenant    int64
		DeletedAt value.Nullable[temporal.DateTime]
	}
	var tenant orm.Number[record, int64]
	r := orm.Define("position_records", func(b orm.Builder[record], m *record) orm.Table[record, int64] {
		tenant = orm.NumberOf(b, &m.Tenant)
		orm.NullableTimeOf(b, &m.DeletedAt)
		return orm.TableOf(b, &m.ID)
	})
	q := r.Query().Where(tenant.Eq(7)).OrderBy(r.Key().Asc())
	first, err := orm.CursorPaginate(ctx, db, q, query.CursorRequest[record]{Size: 1})
	must(t, err)
	token, ok := first.Next.Get()
	if !ok {
		t.Fatal("missing next position")
	}
	for _, boundary := range []int{20, 30} {
		t.Run(strconv.Itoa(boundary), func(t *testing.T) {
			raw, err := base64.RawURLEncoding.DecodeString(token.Token())
			must(t, err)
			// Retain the envelope and scope verbatim; edit only the integer position.
			var envelope map[string]json.RawMessage
			must(t, json.Unmarshal(raw, &envelope))
			scope := string(envelope["scope"])
			var positions []map[string]string
			must(t, json.Unmarshal(envelope["values"], &positions))
			if len(positions) != 1 || positions[0]["k"] != "int" {
				t.Fatal("unexpected position", positions)
			}
			positions[0]["t"] = strconv.Itoa(boundary)
			envelope["values"], err = json.Marshal(positions)
			must(t, err)
			if string(envelope["scope"]) != scope {
				t.Fatal("scope changed")
			}
			raw, err = json.Marshal(envelope)
			must(t, err)
			edited, err := query.ParseCursor[record](base64.RawURLEncoding.EncodeToString(raw))
			must(t, err)
			page, err := orm.CursorPaginate(ctx, db, q, query.CursorRequest[record]{Size: 10, After: value.Set(edited)})
			must(t, err)
			var ids []int64
			for _, row := range page.Items {
				if row.Tenant != 7 || !row.DeletedAt.IsNull() {
					t.Fatal("out-of-scope row", row)
				}
				ids = append(ids, row.ID)
			}
			if !reflect.DeepEqual(ids, []int64{40, 50}) {
				t.Fatal("edited position rows", ids)
			}
		})
	}
}
