package ormwritetest

import "github.com/weiloon1234/Foundry-Go/database/orm"

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
			r := orm.Define("secure_records", func(b orm.Builder[secureRecord], m *secureRecord) orm.Table[secureRecord, model.ID[secureRecord]] {
				if nullable {
					orm.EncryptedTextOf(b, &m.Secret)
					orm.ColumnOf(b, &m.Backup, orm.Codec(codec.Nullable(encrypted.TextCodec())))
				} else {
					orm.ColumnOf(b, &m.Secret, orm.Codec(encrypted.TextCodec()))
					orm.NullableEncryptedTextOf(b, &m.Backup)
				}
				return orm.TableOf(b, &m.ID)
			})
			field := "Secret"
			if nullable {
				field = "Backup"
			}
			if err := orm.Check(r); err == nil || !strings.Contains(err.Error(), field) || !strings.Contains(err.Error(), "EncryptedTextOf/EncryptedJSONOf") {
				t.Fatalf("missing declaration advice: %v", err)
			}
			var statements []string
			db := testDB(t, testConnector{statements: &statements})
			if _, err := orm.All(t.Context(), db, r.Query()); err == nil {
				t.Fatal("invalid declaration executed")
			}
			if _, err := orm.Count(t.Context(), db, r.Query()); err == nil {
				t.Fatal("invalid declaration counted")
			}
			if _, err := orm.Exists(t.Context(), db, r.Query()); err == nil {
				t.Fatal("invalid declaration checked existence")
			}
			if _, err := orm.Raw(t.Context(), db, r, "must not execute"); err == nil {
				t.Fatal("invalid encrypted declaration executed raw SQL")
			}
			if len(statements) != 0 {
				t.Fatal("invalid declaration sent SQL")
			}
		})
	}
}
