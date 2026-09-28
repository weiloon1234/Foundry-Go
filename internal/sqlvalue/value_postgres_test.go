package sqlvalue_test

import (
	"database/sql/driver"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/sqlvalue"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func TestSQLSnapshotPreservesPostgresByteNullness(t *testing.T) {
	db := pgtest.Open(t)
	for _, input := range []driver.Value{nil, []byte(nil), []byte{}, []byte("data")} {
		encoded, err := sqlvalue.Encode(input)
		if err != nil {
			t.Fatal(err)
		}
		restored, err := encoded.Decode()
		if err != nil {
			t.Fatal(err)
		}
		// This read-only probe checks the actual driver/database boundary. It
		// creates no table and changes no application or integration data.
		rows, err := db.Query(t.Context(), `SELECT $1::bytea IS NULL, $2::bytea IS NULL, $1::bytea IS NOT DISTINCT FROM $2::bytea`, input, restored)
		if err != nil {
			t.Fatal(err)
		}
		if !rows.Next() {
			t.Fatal("missing byte comparison", rows.Err(), rows.Close())
		}
		var originalNull, restoredNull, equal bool
		if err := rows.Scan(&originalNull, &restoredNull, &equal); err != nil {
			t.Fatal(err)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
		if originalNull != restoredNull || !equal {
			t.Fatal("SQL snapshot changed PostgreSQL byte NULL semantics")
		}
	}
}
