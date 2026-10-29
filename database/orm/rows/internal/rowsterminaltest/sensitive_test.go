package rowsterminaltest

import "github.com/weiloon1234/Foundry-Go/database/orm/rows"

import (
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/encrypted"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestEncryptedProjectionRejectedBeforeSQL(t *testing.T) {
	testEncryptedRow(t, encrypted.TextCodec())
	testEncryptedRow(t, codec.Nullable(encrypted.TextCodec()))
}
func testEncryptedRow[V any](t *testing.T, c codec.Codec[V]) {
	t.Helper()
	schema := rows.Define(func(b rows.Builder[cell[V]], r *cell[V]) rows.Row[cell[V]] {
		rows.FieldOf(b, &r.Value, orm.Codec(c))
		return b.Row()
	})
	if err := rows.Check(schema); err == nil || !strings.Contains(err.Error(), "Value: encrypted values cannot be opened by projections") {
		t.Fatalf("missing encrypted projection error: %v", err)
	}
	state := &driverState{}
	db := openTest(t, state)
	source := rows.As[left](declareMembers().Query(), "m")
	if _, err := rows.Read(rows.Select(source, schema)).All(t.Context(), db); err == nil {
		t.Fatal("encrypted projection executed")
	}
	if _, err := rows.Raw(t.Context(), db, schema, "must not execute"); err == nil {
		t.Fatal("encrypted raw row executed")
	}
	if state.sql != "" {
		t.Fatal("encrypted projection sent SQL")
	}
}

func TestNullableEncryptedProjectionRejected(t *testing.T) {
	schema := rows.Define(func(b rows.Builder[cell[value.Nullable[encrypted.Text]]], r *cell[value.Nullable[encrypted.Text]]) rows.Row[cell[value.Nullable[encrypted.Text]]] {
		rows.NullableFieldOf(b, &r.Value, orm.Codec(encrypted.TextCodec()))
		return b.Row()
	})
	if err := rows.Check(schema); err == nil {
		t.Fatal("nullable encrypted projection accepted")
	}
}
