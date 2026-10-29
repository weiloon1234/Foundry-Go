package orm

import (
	"database/sql/driver"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type positionRecord struct {
	ID        int64
	Tenant    int64
	DeletedAt value.Nullable[temporal.DateTime]
}

func TestEditedCursorPositionPreservesPredicates(t *testing.T) {
	var tenant Number[positionRecord, int64]
	r := Define("position_records", func(b Builder[positionRecord], m *positionRecord) Table[positionRecord, int64] {
		tenant = NumberOf(b, &m.Tenant)
		SoftDeleteOf(b, &m.DeletedAt)
		return TableOf(b, &m.ID)
	})
	q := r.Query().Where(NumberEq(tenant, 7)).OrderBy(KeyAsc(r.Key()))
	initial := testDB(t, testConnector{columns: []string{"id", "tenant", "deleted_at"}, values: [][]driver.Value{{int64(1), int64(7), nil}, {int64(40), int64(7), nil}}})
	first, err := CursorPaginate(t.Context(), initial, q, CursorRequest[positionRecord]{Size: 1})
	if err != nil {
		t.Fatal(err)
	}
	token, ok := first.Next.Get()
	if !ok {
		t.Fatal("missing next position")
	}
	for _, boundary := range []int64{20, 30} {
		envelope, err := querycore.DecodeCursor(token.Token())
		if err != nil {
			t.Fatal(err)
		}
		scope := envelope.Scope
		envelope.Values[0], err = querycore.EncodeCursorValue(boundary)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		edited, err := ParseCursor[positionRecord](base64.RawURLEncoding.EncodeToString(encoded))
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := querycore.DecodeCursor(edited.Token())
		if err != nil || decoded.Scope != scope {
			t.Fatal("scope changed", err)
		}
		calls := 0
		db := testDB(t, testConnector{respondBound: func(sql string, args []driver.NamedValue) ([]string, [][]driver.Value, error) {
			calls++
			// The driver is not a SQL engine: pin both original predicates AND the
			// edited seek in the emitted statement; pgsmoke evaluates them in PostgreSQL.
			if !strings.Contains(sql, `"tenant" = $1`) || !strings.Contains(sql, `"deleted_at" IS NULL`) || !strings.Contains(sql, `"id" > $2`) || strings.Contains(sql, " OR ") {
				t.Fatalf("position lost query predicates: %s", sql)
			}
			if len(args) < 2 || args[0].Value != int64(7) || args[1].Value != boundary {
				t.Fatalf("bindings: %v", args)
			}
			return []string{"id", "tenant", "deleted_at"}, [][]driver.Value{{int64(40), int64(7), nil}}, nil
		}})
		page, err := CursorPaginate(t.Context(), db, q, CursorRequest[positionRecord]{Size: 1, After: value.Set(edited)})
		if err != nil || calls != 1 || len(page.Items) != 1 {
			t.Fatalf("edited position: %+v %v calls=%d", page, err, calls)
		}
		if got := page.Items[0]; got.ID != 40 || got.Tenant != 7 || !got.DeletedAt.IsNull() {
			t.Fatal("out-of-scope row", got)
		}
	}
}
