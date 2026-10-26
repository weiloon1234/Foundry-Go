package orm

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/extensionslot"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

type externalValue struct {
	Loaded bool
	Value  string
}
type extensionModel struct {
	ID       int64
	External externalValue
}
type extensionTable struct {
	Table[extensionModel, int64]
	External Slot[extensionModel, externalValue]
}

func extensionFields() extensionTable {
	return Define("adapter_owners", func(b Builder[extensionModel], m *extensionModel) extensionTable {
		return extensionTable{TableOf(b, &m.ID), SlotOf(b, &m.External)}
	})
}
func TestExtensionAdapterBatchAndMissingParity(t *testing.T) {
	for _, missing := range []bool{false, true} {
		for _, limit := range []int{10, 2} {
			fields := extensionFields()
			parents := []extensionModel{{ID: 1}, {ID: 2, External: externalValue{Loaded: true, Value: "retained"}}, {ID: 3}, {ID: 4}, {ID: 5}}
			var batches []int
			binding := extensionslot.Binding[extensionModel, externalValue]{Name: "External", Table: "adapter_owners", Get: func(m extensionModel) externalValue { return m.External }, Set: func(m extensionModel, v externalValue) extensionModel { m.External = v; return m }, Loaded: func(v externalValue) bool { return v.Loaded }, Count: func(externalValue) int { return 1 }, Fetch: func(_ context.Context, _ database.Executor, parents []extensionModel) ([]externalValue, error) {
				batches = append(batches, len(parents))
				out := make([]externalValue, len(parents))
				for i := range out {
					out[i] = externalValue{true, "loaded"}
				}
				return out, nil
			}}
			db := hookDB(t, testConnector{})
			limits := DefaultRelationLimits()
			limits.BatchSize = 2
			limits.MaxRows = limit
			p := FoundryExtension(fields.External, extensionslot.New(binding), sqlowner.Queries())
			p.plan = relationWithLimits(p.plan, &limits)
			var result []extensionModel
			var err error
			if missing {
				result, err = LoadMissingFrom(t.Context(), db, fields.Query().With(p), parents)
			} else {
				result, err = LoadFrom(t.Context(), db, fields.Query().With(p), parents)
			}
			if parents[0].External.Loaded {
				t.Fatal("caller mutated")
			}
			outcome := struct {
				Rows    []extensionModel
				Batches []int
				Invalid bool
			}{result, batches, errors.Is(err, fault.Invalid)}
			frozenMatch(t, fmt.Sprintf("extension_%t_%d", missing, limit), outcome)
			if limit == 10 && len(batches) != (5-btoi(missing)+1)/2 {
				t.Fatal("batch count", batches)
			}
		}
	}
}
func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}
func TestExtensionAdapterRejectsInvalidBindingsAndZeroCapabilities(t *testing.T) {
	fields := extensionFields()
	_, ref, get, set, err := FoundrySlot(fields.Key(), fields.External, sqlowner.MetadataSeal{})
	if !errors.Is(err, fault.Invalid) || ref != nil || get != nil || set != nil {
		t.Fatal("zero capability escaped")
	}
	if err := Check(FoundryExtension(fields.External, extensionslot.Slot[extensionModel]{}, sqlowner.QuerySeal{})); !errors.Is(err, fault.Invalid) {
		t.Fatal("zero query capability escaped")
	}
	if err := Check(FoundryExtension(fields.External, extensionslot.Slot[extensionModel]{}, sqlowner.Queries())); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	other := extensionFields()
	if _, _, _, _, err := FoundrySlot(other.Key(), fields.External, sqlowner.Metadata()); !errors.Is(err, fault.Invalid) {
		t.Fatal("foreign key accepted")
	}
	slot := extensionslot.New(extensionslot.Binding[extensionModel, string]{Name: "External", Table: "adapter_owners", Get: func(extensionModel) string { return "" }, Set: func(m extensionModel, _ string) extensionModel { return m }, Loaded: func(string) bool { return false }, Count: func(string) int { return 1 }, Fetch: func(_ context.Context, _ database.Executor, m []extensionModel) ([]string, error) {
		return make([]string, len(m)), nil
	}})
	if err := Check(FoundryExtension(fields.External, slot, sqlowner.Queries())); !errors.Is(err, fault.Invalid) {
		t.Fatal("wrong slot value accepted", err)
	}
}
func TestIdentityAdapterParity(t *testing.T) {
	fields := extensionFields()
	current := IdentityOf(fields.Table, fields.Key())
	if err := current.Validate(); err != nil {
		t.Fatal(err)
	}
	identity, err := current.Reference(7).Identity()
	if err != nil {
		t.Fatal(err)
	}
	token, err := current.KeyToken(7)
	if err != nil {
		t.Fatal(err)
	}
	frozenMatch(t, "identity", []any{identity, token})
	for _, mode := range []string{"active", "retained", "lock"} {
		t.Run(mode, func(t *testing.T) {
			var sql []string
			begins := 0
			db := hookDB(t, testConnector{statements: &sql, onBegin: func() { begins++ }, columns: []string{"id"}, values: [][]driver.Value{{int64(7)}}})
			var keys []int64
			var err error
			switch mode {
			case "active":
				keys, err = current.ActiveKeys(t.Context(), db, []int64{7})
			case "retained":
				keys, err = current.RetainedKeys(t.Context(), db, []int64{7})
			case "lock":
				err = db.Transaction(t.Context(), func(tx *database.Tx) error {
					found, lockErr := current.LockActive(t.Context(), tx, 7)
					if found {
						keys = []int64{7}
					}
					return lockErr
				})
			}
			if err != nil || !reflect.DeepEqual(keys, []int64{7}) {
				t.Fatal(keys, err)
			}
			if len(sql) != 1 || (mode == "lock" && begins != 1) || (mode != "lock" && begins != 0) {
				t.Fatal("identity statement count", begins, sql)
			}
			frozenMatch(t, "identity_sql", sql)
		})
	}
	if err := IdentityOf(fields.Table, extensionFields().Key()).Validate(); !errors.Is(err, fault.Invalid) {
		t.Fatal("foreign declaration accepted")
	}
}
func TestUniqueAdapterPreservesScopeAndDuplicateFailure(t *testing.T) {
	fields := keyedFields()
	for _, count := range []int{0, 1, 2} {
		rows := make([][]driver.Value, count)
		for i := range rows {
			rows[i] = []driver.Value{int64(i + 1), "tenant", "name"}
		}
		var sql []string
		var args [][]any
		db := hookDB(t, testConnector{statements: &sql, bindings: &args, columns: []string{"id", "scope", "name"}, values: rows})
		got, err := Unique(fields.Query().Where(fields.Scope.Eq("tenant")), fields.Name).Find(t.Context(), db, "name")
		if count == 2 {
			if !errors.Is(err, fault.Internal) || got.IsSet() {
				t.Fatal("duplicate escaped", err)
			}
		} else if err != nil || got.IsSet() != (count == 1) {
			t.Fatal(got, err)
		}
		if len(sql) != 1 || len(args[0]) != 3 || args[0][0] != "tenant" || args[0][1] != "name" {
			t.Fatal("scope or statement count", sql, args)
		}
	}
	if err := Unique(fields.Query().Limit(1), fields.Name).Validate(); !errors.Is(err, fault.Invalid) {
		t.Fatal("hidden duplicate", err)
	}
}

func TestInsertEachLifecycleCountsAndUnknownOutcome(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		var sql []string
		var trace []string
		begins := 0
		fields := keyedFields(WithHooks(func() Hooks[keyedRow] {
			return Hooks[keyedRow]{Creating: func(_ context.Context, _ *database.Tx, _ *Write[keyedRow]) error {
				trace = append(trace, "creating")
				return nil
			}, Created: func(_ context.Context, _ *database.Tx, _ Changes[keyedRow]) error {
				trace = append(trace, "created")
				return nil
			}}
		}))
		connector := testConnector{statements: &sql, onBegin: func() { begins++ }, columns: []string{"id", "scope", "name"}, values: [][]driver.Value{{int64(1), "tenant", "stored"}}}
		if unknown {
			connector.commitErr = errors.New("commit acknowledgement lost")
		}
		db := hookDB(t, connector)
		draft := []Assignment[keyedRow]{fields.Key().Set(1), fields.Scope.Set("tenant"), fields.Name.Set("input")}
		result, err := InsertEach(t.Context(), db, fields.Table, [][]Assignment[keyedRow]{draft, draft})
		if unknown {
			var outcome *WriteError[[]keyedRow]
			if !errors.As(err, &outcome) || len(outcome.Candidate()) != 2 || result != nil {
				t.Fatal("lost reconciliation outcome", err)
			}
		} else if err != nil || len(result) != 2 {
			t.Fatal(result, err)
		}
		if begins != 1 || len(sql) != 2 || !reflect.DeepEqual(trace, []string{"creating", "created", "creating", "created"}) {
			t.Fatal("batch lifecycle or statement count", begins, sql, trace)
		}
	}
}

type fakeLookupColumn struct{ KeyColumn[keyedRow, string] }
type promotedLookupColumn struct{ Text[keyedRow, string] }

func TestUniqueRejectsPromotedColumnsBeforeDispatch(t *testing.T) {
	fields := keyedFields()
	for _, field := range []KeyColumn[keyedRow, string]{fakeLookupColumn{}, fakeLookupColumn{fields.Name}, promotedLookupColumn{fields.Name}} {
		if err := Unique(fields.Query(), field).Validate(); !errors.Is(err, fault.Invalid) {
			t.Fatal("foreign column wrapper accepted", err)
		}
	}
}

func TestLookupDiagnosticsRedactScopes(t *testing.T) {
	p := people()
	scoped := p.Query().Where(p.Name.Eq("private-scope-literal"))
	keyed := Keyed(scoped, p.ID)
	unique := Unique(scoped, p.Name)
	related := RelatedUnique(BelongsToOf(p.Manager, p.ManagerID, p.ID).Where(p.Name.Eq("private-scope-literal")), p.Name)
	for _, tc := range []struct {
		value any
		text  string
	}{
		{keyed, "unique model lookup"}, {&keyed, "unique model lookup"},
		{unique, "unique model lookup"}, {&unique, "unique model lookup"},
		{related, "related model lookup"}, {&related, "related model lookup"},
	} {
		for _, verb := range []string{"%v", "%+v", "%#v"} {
			if got := fmt.Sprintf(verb, tc.value); got != tc.text {
				t.Errorf("%s leaked lookup diagnostics: %q", verb, got)
			}
		}
		if got := tc.value.(fmt.Stringer).String(); got != tc.text {
			t.Fatal(got)
		}
		if got := tc.value.(fmt.GoStringer).GoString(); got != tc.text {
			t.Fatal(got)
		}
	}
}

// Unexported fields prevent fmt from dispatching the lookup's own formatters.
type privateLookupWrapper[T any] struct{ lookup T }
type nestedLookupWrapper[T any] struct{ inner privateLookupWrapper[T] }

func TestLookupPrivateDiagnosticsRedactScopes(t *testing.T) {
	p := people()
	const secret = "private-scope-literal"
	scoped := p.Query().Where(p.Name.Eq(secret))
	keyed := Keyed(scoped, p.ID)
	unique := Unique(scoped, p.Name)
	related := RelatedUnique(BelongsToOf(p.Manager, p.ManagerID, p.ID).Where(p.Name.Eq(secret)), p.Name)
	keyedWrapper := privateLookupWrapper[KeyLookup[relationPerson, int64]]{keyed}
	uniqueWrapper := privateLookupWrapper[KeyLookup[relationPerson, string]]{unique}
	relatedWrapper := privateLookupWrapper[RelatedLookup[relationPerson, relationPerson, string]]{related}
	cause := errors.New("lookup failed")
	for name, wrapper := range map[string]any{
		"keyed":           keyedWrapper,
		"unique":          uniqueWrapper,
		"related":         relatedWrapper,
		"keyed pointer":   &keyedWrapper,
		"unique pointer":  &uniqueWrapper,
		"related pointer": &relatedWrapper,
		"nested keyed":    nestedLookupWrapper[KeyLookup[relationPerson, int64]]{keyedWrapper},
		"nested unique":   nestedLookupWrapper[KeyLookup[relationPerson, string]]{uniqueWrapper},
		"nested related":  nestedLookupWrapper[RelatedLookup[relationPerson, relationPerson, string]]{relatedWrapper},
	} {
		t.Run(name, func(t *testing.T) {
			for _, verb := range []string{"%v", "%+v", "%#v"} {
				if got := fmt.Sprintf(verb, wrapper); strings.Contains(got, secret) {
					t.Errorf("%s exposed a retained predicate", verb)
				}
				err := fmt.Errorf("lookup "+verb+": %w", wrapper, cause)
				if !errors.Is(err, cause) {
					t.Fatal("diagnostic lost its cause")
				}
				for _, errorVerb := range []string{"%v", "%+v", "%#v"} {
					if strings.Contains(fmt.Sprintf(errorVerb, err), secret) {
						t.Errorf("%s diagnostic exposed a retained predicate under %s", verb, errorVerb)
					}
				}
			}
		})
	}
}

func TestRelatedLookupReuseKeepsParentScopesIndependent(t *testing.T) {
	p := people()
	lookup := RelatedUnique(HasManyOf(p.Reports, p.ID, p.ManagerID).Where(p.Name.Eq("scope")), p.Name)
	copy := lookup
	var statements []string
	var bindings [][]any
	db := hookDB(t, testConnector{statements: &statements, bindings: &bindings, columns: personColumns})
	for i, current := range []RelatedLookup[relationPerson, relationPerson, string]{lookup, copy, lookup} {
		if _, err := current.Find(t.Context(), db, relationPerson{ID: int64(i + 1)}, "name"); err != nil {
			t.Fatal(err)
		}
	}
	want := [][]any{
		{"scope", int64(1), "name", int64(2)},
		{"scope", int64(2), "name", int64(2)},
		{"scope", int64(3), "name", int64(2)},
	}
	if len(statements) != 3 || statements[0] != statements[1] || statements[0] != statements[2] || !reflect.DeepEqual(bindings, want) {
		t.Fatal("reused lookup changed another call's scope", statements, bindings)
	}
}

func TestZeroLookupsFailClosed(t *testing.T) {
	var keyed KeyLookup[relationPerson, int64]
	var related RelatedLookup[relationPerson, relationPerson, string]
	if !errors.Is(keyed.Validate(), fault.Invalid) || !errors.Is(related.Validate(), fault.Invalid) {
		t.Fatal("zero lookup accepted")
	}
	var statements []string
	db := hookDB(t, testConnector{statements: &statements})
	if _, err := keyed.Find(t.Context(), db, 1); !errors.Is(err, fault.Invalid) {
		t.Fatal("zero keyed lookup accepted", err)
	}
	if _, err := related.Find(t.Context(), db, relationPerson{ID: 1}, "name"); !errors.Is(err, fault.Invalid) {
		t.Fatal("zero related lookup accepted", err)
	}
	if len(statements) != 0 {
		t.Fatal("zero lookup executed SQL")
	}
}

func TestNativeExtensionBridgeRequiresCapabilityAndSlotType(t *testing.T) {
	fields := extensionFields()
	source := extensionslot.New(extensionslot.Binding[extensionModel, externalValue]{Name: "External", Table: "adapter_owners", Get: func(m extensionModel) externalValue { return m.External }, Set: func(m extensionModel, v externalValue) extensionModel { m.External = v; return m }, Loaded: func(v externalValue) bool { return v.Loaded }, Count: func(externalValue) int { return 1 }, Fetch: func(context.Context, database.Executor, []extensionModel) ([]externalValue, error) {
		t.Fatal("validation dispatched a loader")
		return nil, nil
	}})
	for _, relation := range []ExtensionRelation[extensionModel]{FoundryExtension(fields.External, source, sqlowner.QuerySeal{}), FoundryExtension(fields.External, extensionslot.Slot[extensionModel]{}, sqlowner.Queries())} {
		if err := Check(relation); !errors.Is(err, fault.Invalid) {
			t.Fatalf("invalid bridge accepted: %v", err)
		}
	}
	if err := Check(FoundryExtension(fields.External, source, sqlowner.Queries())); err != nil {
		t.Fatal(err)
	}
	wrongValue := extensionslot.New(extensionslot.Binding[extensionModel, string]{Name: "External", Table: "adapter_owners", Get: func(extensionModel) string { return "" }, Set: func(m extensionModel, _ string) extensionModel { return m }, Loaded: func(string) bool { return false }, Count: func(string) int { return 1 }, Fetch: func(context.Context, database.Executor, []extensionModel) ([]string, error) {
		t.Fatal("wrong-value loader dispatched")
		return nil, nil
	}})
	if err := Check(FoundryExtension(fields.External, wrongValue, sqlowner.Queries())); !errors.Is(err, fault.Invalid) {
		t.Fatal("wrong slot value accepted", err)
	}
}
