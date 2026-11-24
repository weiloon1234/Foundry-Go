package orm

import (
	"bytes"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm/internal/declarationsnapshot"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

type snapshotRecord struct {
	ID   int64
	Name string
}
type snapshotTable struct {
	Table[snapshotRecord, int64]
	Name Text[snapshotRecord, string]
}

func snapshotHooks() Hooks[snapshotRecord] { return Hooks[snapshotRecord]{} }
func snapshotDeclare(b Builder[snapshotRecord], r *snapshotRecord) snapshotTable {
	return snapshotTable{TableOf(b, &r.ID), TextOf(b, &r.Name)}
}
func TestDeclarationSnapshot(t *testing.T) {
	a := Define("records", snapshotDeclare, WithHooks(snapshotHooks))
	b := Define("records", snapshotDeclare, WithHooks(snapshotHooks))
	before, err := DeclarationSnapshot(a, "records", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	after, err := DeclarationSnapshot(b, "records", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("equal declarations differ: %s\n%s", before, after)
	}
	changed := Define("records", snapshotDeclare)
	after, err = DeclarationSnapshot(changed, "records", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	differences, err := FoundryCompareDeclarationSnapshots(before, after, sqlowner.Metadata())
	if err != nil {
		t.Fatal(err)
	}
	if len(differences) != 1 || differences[0] != "hooks" {
		t.Fatal(differences)
	}
	if !strings.Contains(string(before), "snapshotHooks") {
		t.Fatal("original factory identity was erased")
	}
	if _, err = FoundryCompareDeclarationSnapshots(before, after, sqlowner.MetadataSeal{}); err == nil {
		t.Fatal("accepted missing capability")
	}
}

type snapshotInputTable struct {
	Table[snapshotRecord, int64]
	Name InputText[snapshotRecord, string, string]
}

func snapshotNormalize(s string) (string, error)      { return s, nil }
func snapshotOtherNormalize(s string) (string, error) { return s, nil }
func TestDeclarationSnapshotMutatorIdentity(t *testing.T) {
	declare := func(transform func(string) (string, error)) snapshotInputTable {
		return Define("records", func(b Builder[snapshotRecord], r *snapshotRecord) snapshotInputTable {
			return snapshotInputTable{TableOf(b, &r.ID), InputTextOf(b, &r.Name, transform)}
		})
	}
	before, err := DeclarationSnapshot(declare(snapshotNormalize), "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	after, err := DeclarationSnapshot(declare(snapshotOtherNormalize), "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := FoundryCompareDeclarationSnapshots(before, after, sqlowner.Metadata())
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != "columns[1].mutator" {
		t.Fatalf("mutator identity difference: %v", paths)
	}
}

func TestDeclarationSnapshotFlags(t *testing.T) {
	table := Define("records", func(b Builder[snapshotRecord], r *snapshotRecord) snapshotTable {
		return snapshotTable{TableOf(b, &r.ID), TextOf(b, &r.Name, Codec(codec.String[string]().WithSensitiveValues()))}
	})
	data, err := DeclarationSnapshot(table, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := declarationsnapshot.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Columns[1].Sensitive || snapshot.Columns[1].NonCollatable {
		t.Fatalf("sensitive text flags lost: %+v", snapshot.Columns[1])
	}
	if snapshot.Columns[0].Sensitive || !snapshot.Columns[0].NonCollatable {
		t.Fatalf("numeric flags lost: %+v", snapshot.Columns[0])
	}
}

func TestDeclarationSnapshotMemberBinding(t *testing.T) {
	type record struct {
		ID            int64
		First, Second string
	}
	type table struct {
		Table[record, int64]
		First, Second Text[record, string]
	}
	declare := func(swapped bool) table {
		return Define("records", func(b Builder[record], m *record) table {
			base := TableOf(b, &m.ID)
			first, second := TextOf(b, &m.First), TextOf(b, &m.Second)
			if swapped {
				first, second = second, first
			}
			return table{base, first, second}
		})
	}
	before, err := DeclarationSnapshot(declare(false), "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	after, err := DeclarationSnapshot(declare(true), "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	differences, err := FoundryCompareDeclarationSnapshots(before, after, sqlowner.Metadata())
	if err != nil {
		t.Fatal(err)
	}
	if len(differences) != 2 || differences[0] != "members[0].field" || differences[1] != "members[1].field" {
		t.Fatal(differences)
	}
	snapshot, err := declarationsnapshot.Decode(before)
	if err != nil {
		t.Fatal(err)
	}
	member := snapshot.Members[0]
	if member.Input != "string" || !strings.HasSuffix(member.Kind, ".RequiredTextKind") || !strings.HasSuffix(member.Role, ".OrdinaryRole") {
		t.Fatalf("lost member capabilities: %+v", member)
	}
}
