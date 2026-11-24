package orm

import (
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/weiloon1234/Foundry-Go/database/orm/internal/declarationsnapshot"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

// DeclarationSnapshot exists only for foundry models convert and is removed after migration.
// It captures owned, read-only conversion metadata.
// Extension descriptions come from their existing owners.
func DeclarationSnapshot(table CheckedTable, identity string, slots []DeclarationSlot, owners []DeclarationOwner) ([]byte, error) {
	source, ok := table.(interface{ ormSnapshotCore() *tableCore })
	if !ok || source.ormSnapshotCore() == nil {
		return nil, invalid("snapshot requires a declared table")
	}
	core := source.ormSnapshotCore()
	if err := tableError(core); err != nil {
		return nil, err
	}
	t := declarationsnapshot.Table{Name: core.model.Table, Model: snapshotType(core.typ), Identity: identity, Hooks: core.snapshotHooks, Scopes: core.snapshotScopes, Retrieval: core.snapshotRetrieval, NoTimestamps: core.snapshotNoTimestamps, NoSoftDeletes: core.snapshotNoSoftDeletes}
	if core.primary != nil {
		t.Key = snapshotType(core.primary.field.Type)
		t.KeyCodec = core.primary.snapshotCodec
	}
	if core.model.Timestamps != nil {
		t.Created = core.model.Timestamps.Created
		t.Updated = core.model.Timestamps.Updated
	}
	if core.model.SoftDelete != nil {
		t.Deleted = core.model.SoftDelete.Name
	}
	for _, c := range core.columns {
		input := c.stored
		if c.mutator.Input != nil {
			input = c.mutator.Input
		}
		t.Columns = append(t.Columns, declarationsnapshot.Column{Field: c.field.Name, Name: c.metadata.Name, Stored: snapshotType(c.field.Type), Input: snapshotType(input), Nullable: c.metadata.Nullable, DatabaseDefault: c.metadata.DatabaseDefault, Role: c.role, Encrypted: c.open != nil, Sensitive: c.sensitive, Parameter: strconv.Itoa(int(c.kind)), NonCollatable: c.metadata.NonCollatable, Codec: c.snapshotCodec, Mutator: conversionMutator(c)})
	}
	value := reflect.ValueOf(table)
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	if value.Kind() == reflect.Struct {
		for i := 0; i < value.NumField(); i++ {
			field := value.Type().Field(i)
			member := declarationsnapshot.Member{Name: field.Name, Type: snapshotType(field.Type)}
			if column, ok := snapshotColumnMember(value.Field(i)); ok {
				member = column
				member.Name = field.Name
				member.Type = snapshotType(field.Type)
			}
			t.Members = append(t.Members, member)
		}
	}
	sort.Slice(t.Members, func(i, j int) bool { return t.Members[i].Name < t.Members[j].Name })
	indices := make([]int, 0, len(core.slots))
	for i := range core.slots {
		indices = append(indices, i)
	}
	sort.Ints(indices)
	used := make(map[string]bool)
	for _, i := range indices {
		s := core.slots[i]
		slot := declarationsnapshot.Slot{Field: s.name, Type: snapshotType(core.typ.Field(i).Type), Target: snapshotType(s.target), Pivot: snapshotType(s.pivot)}
		for _, description := range slots {
			if description.Field == s.name {
				if slot.Extension != nil {
					return nil, invalid("duplicate extension description")
				}
				copy := description
				slot.Extension = &copy
				used[s.name] = true
			}
		}
		// Relation slots have no external storage description.
		if core.typ.Field(i).Type.PkgPath() != "github.com/weiloon1234/Foundry-Go/database/relation" && slot.Extension == nil {
			return nil, invalid("extension slot snapshot requires its stored description")
		}
		t.Slots = append(t.Slots, slot)
	}
	for _, s := range slots {
		if !used[s.Field] {
			return nil, invalid("extension description has no matching slot")
		}
	}
	t.Owners = append([]DeclarationOwner(nil), owners...)
	sort.Slice(t.Owners, func(i, j int) bool { return t.Owners[i].Name < t.Owners[j].Name })
	return declarationsnapshot.Encode(t)
}

// FoundryCompareDeclarationSnapshots exists only for foundry models convert and
// is removed after migration. It compares the typed ORM snapshot schema.
func FoundryCompareDeclarationSnapshots(before, after []byte, seal sqlowner.MetadataSeal) ([]string, error) {
	if !seal.Valid() {
		return nil, invalid("invalid declaration snapshot capability")
	}
	a, err := declarationsnapshot.Decode(before)
	if err != nil {
		return nil, err
	}
	b, err := declarationsnapshot.Decode(after)
	if err != nil {
		return nil, err
	}
	return declarationsnapshot.Compare(a, b), nil
}

// FoundryExplainDeclarationSnapshots describes metadata differences for conversion.
// It exists only for foundry models convert; remove it after migration.
func FoundryExplainDeclarationSnapshots(before, after []byte, seal sqlowner.MetadataSeal) ([]string, error) {
	if !seal.Valid() {
		return nil, invalid("invalid declaration snapshot capability")
	}
	a, err := declarationsnapshot.Decode(before)
	if err != nil {
		return nil, err
	}
	b, err := declarationsnapshot.Decode(after)
	if err != nil {
		return nil, err
	}
	return declarationsnapshot.Explain(a, b), nil
}

func (t Table[M, K]) ormSnapshotCore() *tableCore { return t.core }

// Inspect only ORM-owned handle metadata. Declaration handles remain methodless;
// no reflection or metadata lookup is added to their query operations.
func snapshotColumnMember(value reflect.Value) (declarationsnapshot.Member, bool) {
	typ := value.Type()
	if typ.PkgPath() != reflect.TypeFor[RequiredScalarKind]().PkgPath() || !strings.HasPrefix(typ.Name(), "ColumnHandle[") {
		return declarationsnapshot.Member{}, false
	}
	input, _ := typ.FieldByName("input")
	kind, _ := typ.FieldByName("kind")
	role, _ := typ.FieldByName("role")
	member := declarationsnapshot.Member{Input: snapshotType(input.Type.In(0)), Kind: snapshotType(kind.Type.Elem()), Role: snapshotType(role.Type.Elem())}
	stored := value.FieldByName("stored")
	if !stored.IsNil() {
		core := stored.Elem().FieldByName("core")
		if !core.IsNil() {
			member.Field = core.Elem().FieldByName("field").FieldByName("Name").String()
		}
	}
	return member, true
}

func snapshotType(t reflect.Type) string {
	if t == nil {
		return ""
	}
	if t.Name() != "" && t.PkgPath() != "" {
		return t.PkgPath() + "." + t.Name()
	}
	return t.String()
}
func snapshotFunction(value reflect.Value) string {
	if !value.IsValid() || value.IsNil() {
		return ""
	}
	f := runtime.FuncForPC(value.Pointer())
	if f == nil {
		return ""
	}
	return f.Name()
}
func snapshotCodec(value reflect.Value) []declarationsnapshot.Function {
	var result []declarationsnapshot.Function
	for i := 0; i < value.NumField(); i++ {
		field := value.Field(i)
		if field.Kind() == reflect.Func {
			result = append(result, declarationsnapshot.Function{Property: value.Type().Field(i).Name, Name: snapshotFunction(field)})
		}
	}
	return result
}

// DeclarationSlot exists only for foundry models convert and is removed after migration.
// It is the stored portion of an extension owner description.
type DeclarationSlot = declarationsnapshot.Extension

// DeclarationOwner exists only for foundry models convert and is removed after migration.
// It preserves extension identity and storage names.
type DeclarationOwner = declarationsnapshot.Owner
