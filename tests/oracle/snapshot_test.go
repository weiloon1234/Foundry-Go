package oracle

import (
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
	"testing"
)

func TestSnapshotHasNoFormattingCollisions(t *testing.T) {
	pairs := [][2]any{
		{constantStringer{1}, constantStringer{2}},
		{[]string(nil), []string{}}, {[]string{"a b", "c"}, []string{"a", "b c"}},
		{map[string]string(nil), map[string]string{}}, {"\xff", "\xfe"}, {int64(1), "1"},
		{struct{ Values []string }{}, struct{ Values []string }{[]string{}}},
		{(*int)(nil), new(int)}, {[]any{nil}, []any{(*int)(nil)}},
	}
	for _, p := range pairs {
		if Snapshot(p[0]) == Snapshot(p[1]) {
			t.Fatalf("snapshot collision: %#v", p)
		}
	}
	a, b := 4, 4
	if Snapshot(&a) != Snapshot(&b) {
		t.Fatal("pointer address captured")
	}
}

type constantStringer struct{ hidden int }

func (constantStringer) String() string { return "same" }

type recursiveSnapshotModel struct {
	Parent relation.One[recursiveSnapshotModel]
}

func TestSnapshotAbsentRecursiveRelationTerminates(t *testing.T) {
	if Snapshot(recursiveSnapshotModel{}) == "" {
		t.Fatal("missing absent model")
	}
}

func TestModelSnapshotPreservesScalarStorageAndRelationBoundaries(t *testing.T) {
	type row struct {
		ID       model.ID[row]
		Amount   decimal.Decimal
		At       temporal.DateTime
		Child    relation.One[row]
		Total    relation.Value[decimal.Decimal]
		Optional value.Nullable[decimal.Decimal]
		scratch  string
		Ignored  string `foundry:"-"`
	}
	id1, _ := model.ParseID[row]("00000000-0000-0000-0000-000000000001")
	id2, _ := model.ParseID[row]("00000000-0000-0000-0000-000000000002")
	d1, _ := decimal.Parse("12.34")
	d2, _ := decimal.Parse("12.35")
	at1, _ := temporal.ParseDateTime("2025-01-02T03:04:05.000001Z")
	at2, _ := temporal.ParseDateTime("2025-01-02T03:04:05.000002Z")
	for _, pair := range [][2]any{
		{row{ID: id1}, row{ID: id2}}, {row{Amount: d1}, row{Amount: d2}}, {row{At: at1}, row{At: at2}},
		{row{Optional: value.Of(d1)}, row{Optional: value.Of(d2)}},
		{row{Total: relation.Computed(d1)}, row{Total: relation.Computed(d2)}},
		{row{Child: relation.Single(value.Set(row{ID: id1}))}, row{Child: relation.Single(value.Set(row{ID: id2}))}},
		{id1, id2}, {d1, d2}, {at1, at2},
	} {
		if ModelSnapshot(pair[0]) == ModelSnapshot(pair[1]) {
			t.Fatal("model snapshot lost a scalar value")
		}
	}
	a := row{ID: id1, scratch: "one", Ignored: "one"}
	b := row{ID: id1, scratch: "two", Ignored: "two"}
	if ModelSnapshot(a) != ModelSnapshot(b) || ModelSnapshot(relation.Single(value.Set(a))) != ModelSnapshot(relation.Single(value.Set(b))) {
		t.Fatal("model-only fields leaked across model boundaries")
	}
	if Snapshot(relation.Single(value.Set(a))) == Snapshot(relation.Single(value.Set(b))) {
		t.Fatal("raw snapshots unexpectedly filtered model fields")
	}
}
