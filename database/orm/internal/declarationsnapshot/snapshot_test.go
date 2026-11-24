package declarationsnapshot

import (
	"slices"
	"testing"
)

func TestEverySnapshotProperty(t *testing.T) {
	baseline := Table{Name: "records", Model: "example.Record", Key: "int64", KeyCodec: []Function{{"encode", "encodeID"}}, Identity: "records", Columns: []Column{{Field: "ID", Name: "id", Stored: "int64", Input: "int64", Role: "key", Codec: []Function{{"decode", "decodeID"}}}, {Field: "Name", Name: "name", Stored: "string", Input: "string"}}, Members: []Member{{Name: "Name", Type: "Text", Field: "Name", Input: "string", Kind: "text", Role: "ordinary"}}, Slots: []Slot{{Field: "Files", Type: "Many", Target: "Record", Extension: &Extension{Field: "Files", Kind: "many", Name: "public-media", Storage: "attachments"}}}, Owners: []Owner{{Name: "records", Model: "records", StorageModel: "record_store"}}, Created: "created_at", Updated: "updated_at", Deleted: "deleted_at", Hooks: "recordHooks", Scopes: "recordScopes", Retrieval: "recordRetrieval"}
	encoded, err := Encode(baseline)
	if err != nil {
		t.Fatal(err)
	}
	equal, err := Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if diff := Compare(baseline, equal); len(diff) > 0 {
		t.Fatal(diff)
	}
	cases := []struct {
		name, path string
		change     func(*Table)
	}{
		{"table", "table", func(s *Table) { s.Name = "other" }}, {"model", "model", func(s *Table) { s.Model = "Other" }}, {"key", "key", func(s *Table) { s.Key = "string" }}, {"key codec", "key_codec[0].name", func(s *Table) { s.KeyCodec[0].Name = "other" }},
		{"sql name", "columns[1].name", func(s *Table) { s.Columns[1].Name = "label" }}, {"order", "columns[0].field", func(s *Table) { s.Columns[0], s.Columns[1] = s.Columns[1], s.Columns[0] }}, {"go field", "columns[1].field", func(s *Table) { s.Columns[1].Field = "Label" }},
		{"stored", "columns[1].stored", func(s *Table) { s.Columns[1].Stored = "Named" }}, {"input", "columns[1].input", func(s *Table) { s.Columns[1].Input = "Input" }}, {"nullable", "columns[1].nullable", func(s *Table) { s.Columns[1].Nullable = true }}, {"role", "columns[1].role", func(s *Table) { s.Columns[1].Role = "created" }}, {"default", "columns[1].database_default", func(s *Table) { s.Columns[1].DatabaseDefault = true }}, {"encryption", "columns[1].encrypted", func(s *Table) { s.Columns[1].Encrypted = true }}, {"codec", "columns[0].codec[0].name", func(s *Table) { s.Columns[0].Codec[0].Name = "other" }},
		{"sensitive", "columns[1].sensitive", func(s *Table) { s.Columns[1].Sensitive = true }}, {"non collatable", "columns[1].non_collatable", func(s *Table) { s.Columns[1].NonCollatable = true }},
		{"timestamp", "no_timestamps", func(s *Table) { s.NoTimestamps = true }}, {"soft deletes", "no_soft_deletes", func(s *Table) { s.NoSoftDeletes = true }}, {"created", "created", func(s *Table) { s.Created = "created" }}, {"updated", "updated", func(s *Table) { s.Updated = "updated" }}, {"deleted", "deleted", func(s *Table) { s.Deleted = "deleted" }}, {"hook removed", "hooks", func(s *Table) { s.Hooks = "" }}, {"scope", "scopes", func(s *Table) { s.Scopes = "other" }}, {"retrieval", "retrieval", func(s *Table) { s.Retrieval = "" }},
		{"slot name", "slots[0].extension.name", func(s *Table) { s.Slots[0].Extension.Name = "private-media" }}, {"slot kind", "slots[0].extension.kind", func(s *Table) { s.Slots[0].Extension.Kind = "one" }}, {"slot storage", "slots[0].extension.storage", func(s *Table) { s.Slots[0].Extension.Storage = "other" }}, {"identity", "identity", func(s *Table) { s.Identity = "other" }}, {"owner storage", "owners[0].storage_model", func(s *Table) { s.Owners[0].StorageModel = "other" }}, {"carrier", "members[0].kind", func(s *Table) { s.Members[0].Kind = "number" }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			changed, err := Decode(encoded)
			if err != nil {
				t.Fatal(err)
			}
			test.change(&changed)
			if paths := Compare(baseline, changed); !slices.Contains(paths, test.path) {
				t.Fatalf("missing %s: %v", test.path, paths)
			}
		})
	}
}

func TestExplainNamesFieldsAndBothValues(t *testing.T) {
	before := Table{Columns: []Column{{Field: "Public", Name: "public", Codec: []Function{{Property: "bind", Name: "scalar codec"}}}}}
	after := Table{Columns: []Column{{Field: "Public", Name: "public", Codec: []Function{{Property: "bind", Name: "bool codec"}}}}}
	differences := Explain(before, after)
	if len(differences) != 1 || differences[0] != `Public: codec differs: hand-written "scalar codec", generated "bool codec" (columns[0].codec[0].name)` {
		t.Fatalf("unhelpful difference: %v", differences)
	}
	after.Columns = nil
	if got := Explain(before, after); len(got) != 1 || got[0] != `columns length differs: hand-written 1, generated 0 (columns.length)` {
		t.Fatalf("length difference: %v", got)
	}
}
