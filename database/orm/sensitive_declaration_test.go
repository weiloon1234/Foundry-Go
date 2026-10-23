package orm

import (
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/encrypted"
	"github.com/weiloon1234/Foundry-Go/model"
)

func TestEncryptedOrdinaryColumnRejectedBeforeSQL(t *testing.T) {
	for _, nullable := range []bool{false, true} {
		t.Run(map[bool]string{false: "required", true: "nullable"}[nullable], func(t *testing.T) {
			r := Define("secure_records", func(b Builder[secureRecord], m *secureRecord) Table[secureRecord, model.ID[secureRecord]] {
				if nullable {
					EncryptedTextOf(b, &m.Secret)
					ColumnOf(b, &m.Backup, Codec(codec.Nullable(encrypted.TextCodec())))
				} else {
					ColumnOf(b, &m.Secret, Codec(encrypted.TextCodec()))
					NullableEncryptedTextOf(b, &m.Backup)
				}
				return TableOf(b, &m.ID)
			})
			field := "Secret"
			if nullable {
				field = "Backup"
			}
			if err := Check(r); err == nil || !strings.Contains(err.Error(), field) || !strings.Contains(err.Error(), "EncryptedTextOf/EncryptedJSONOf") {
				t.Fatalf("missing declaration advice: %v", err)
			}
			var statements []string
			db := testDB(t, testConnector{statements: &statements})
			if _, err := r.Query().All(t.Context(), db); err == nil {
				t.Fatal("invalid declaration executed")
			}
			if _, err := r.Query().Count(t.Context(), db); err == nil {
				t.Fatal("invalid declaration counted")
			}
			if _, err := r.Query().Exists(t.Context(), db); err == nil {
				t.Fatal("invalid declaration checked existence")
			}
			if _, err := r.Raw(t.Context(), db, "must not execute"); err == nil {
				t.Fatal("invalid encrypted declaration executed raw SQL")
			}
			if len(statements) != 0 {
				t.Fatal("invalid declaration sent SQL")
			}
		})
	}
}
