package rows

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
	schema := Define(func(b Builder[cell[V]], r *cell[V]) Row[cell[V]] {
		FieldOf(b, &r.Value, orm.Codec(c))
		return b.Row()
	})
	if err := Check(schema); err == nil || !strings.Contains(err.Error(), "Value: encrypted values cannot be opened by projections") {
		t.Fatalf("missing encrypted projection error: %v", err)
	}
	state := &driverState{}
	db := openTest(t, state)
	source := As[left](declareMembers().Query(), "m")
	if _, err := Select(source, schema).All(t.Context(), db); err == nil {
		t.Fatal("encrypted projection executed")
	}
	if _, err := Raw(t.Context(), db, schema, "must not execute"); err == nil {
		t.Fatal("encrypted raw row executed")
	}
	if state.sql != "" {
		t.Fatal("encrypted projection sent SQL")
	}
}

func TestNullableEncryptedProjectionRejected(t *testing.T) {
	schema := Define(func(b Builder[cell[value.Nullable[encrypted.Text]]], r *cell[value.Nullable[encrypted.Text]]) Row[cell[value.Nullable[encrypted.Text]]] {
		NullableFieldOf(b, &r.Value, orm.Codec(encrypted.TextCodec()))
		return b.Row()
	})
	if err := Check(schema); err == nil {
		t.Fatal("nullable encrypted projection accepted")
	}
}
