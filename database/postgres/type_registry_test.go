package postgres

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type registryValue struct{ value int }
type registryCodec struct {
	pgtype.Int8Codec
	label string
}
type registryEncodePlan struct{ label string }

func (p registryEncodePlan) Encode(any, []byte) ([]byte, error) { return []byte(p.label), nil }
func (c registryCodec) PlanEncode(_ *pgtype.Map, _ uint32, _ int16, value any) pgtype.EncodePlan {
	if _, ok := value.(registryValue); ok {
		return registryEncodePlan{c.label}
	}
	return nil
}
func testTypeRegistry() (*pgtype.Map, *TypeRegistry) {
	m := pgtype.NewMap()
	protectCodecMap(m)
	return m, &TypeRegistry{owner: codecRegistryFor(m)}
}
func TestTypeRegistryRetainsDescriptorAliasesAndInference(t *testing.T) {
	m, registry := testTypeRegistry()
	plain := pgtype.NewMap()
	types := []*pgtype.Type{
		{OID: 81001, Name: "alias_a", Codec: registryCodec{label: "a"}},
		{OID: 81001, Name: "alias_b", Codec: registryCodec{label: "b"}},
		{OID: 81002, Name: "alias_b", Codec: registryCodec{label: "c"}},
	}
	for _, typ := range types {
		plain.RegisterType(typ)
		if err := registry.RegisterType(typ); err != nil {
			t.Fatal("register alias failed")
		}
	}
	for _, name := range []string{"alias_a", "alias_b"} {
		if err := registry.RegisterDefaultPgType(registryValue{}, name); err != nil {
			t.Fatal("map alias failed")
		}
		plain.RegisterDefaultPgType(registryValue{}, name)
		for _, oid := range []uint32{0, 81001, 81002} {
			want, nativeErr := plain.Encode(oid, pgtype.BinaryFormatCode, registryValue{}, nil)
			got, protectedErr := m.Encode(oid, pgtype.BinaryFormatCode, protectValue(m, registryValue{}), nil)
			if nativeErr != nil || protectedErr != nil || string(got) != string(want) {
				t.Fatalf("alias %s OID %d changed native selection", name, oid)
			}
		}
	}
	if named, _ := m.TypeForName("alias_a"); named == types[0] {
		t.Fatal("caller descriptor was retained")
	}
}
func TestTypeRegistryNestedDescriptorsKeepTheirIdentity(t *testing.T) {
	m, registry := testTypeRegistry()
	first := &pgtype.Type{OID: 81003, Name: "first", Codec: registryCodec{label: "first"}}
	last := &pgtype.Type{OID: 81003, Name: "last", Codec: registryCodec{label: "last"}}
	if err := registry.RegisterType(last); err != nil {
		t.Fatal("register alias failed")
	}
	composite := &pgtype.Type{OID: 81004, Name: "record", Codec: &pgtype.CompositeCodec{Fields: []pgtype.CompositeCodecField{{Name: "value", Type: first}}}}
	if err := registry.RegisterType(composite); err != nil {
		t.Fatal("register composite failed")
	}
	owned, _ := m.TypeForOID(composite.OID)
	child := owned.Codec.(protectedCodec).native.(*pgtype.CompositeCodec).Fields[0].Type
	current, _ := m.TypeForOID(first.OID)
	if child == current || child.Name != first.Name || current.Name != last.Name {
		t.Fatal("nested descriptor was resolved or registered by OID")
	}
	if composite.Codec.(*pgtype.CompositeCodec).Fields[0].Type != first {
		t.Fatal("caller descriptor mutated")
	}
	recursive := &pgtype.Type{OID: 81005, Name: "recursive"}
	recursive.Codec = &pgtype.CompositeCodec{Fields: []pgtype.CompositeCodecField{{Name: "self", Type: recursive}}}
	// This synthetic cycle exercises copying only: native pgx cannot register
	// a composite that contains itself without traversing its format graph forever.
	owned = registry.owner.decorate(recursive)
	if owned.Codec.(protectedCodec).native.(*pgtype.CompositeCodec).Fields[0].Type != owned {
		t.Fatal("recursive descriptor identity lost")
	}
}
func TestTypeRegistryLatchesFailureAndCloses(t *testing.T) {
	_, registry := testTypeRegistry()
	failure := registry.RegisterType(nil)
	if failure == nil {
		t.Fatal("invalid registration accepted")
	}
	if next := registry.RegisterDefaultPgType(registryValue{}, "int8"); !errors.Is(next, failure) {
		t.Fatal("ignored registration failure was lost")
	}
	registry.closed = true
	if registry.RegisterType(&pgtype.Type{OID: 81006, Name: "late", Codec: pgtype.Int8Codec{}}) == nil {
		t.Fatal("escaped registrar remained usable")
	}
}

type hostileRegistryCodec struct{ pgtype.Int8Codec }

func (hostileRegistryCodec) PreferredFormat() int16 { panic("private registration callback") }
func TestTypeRegistryContainsCodecRegistrationCallbacks(t *testing.T) {
	_, registry := testTypeRegistry()
	if err := registry.RegisterType(&pgtype.Type{OID: 81007, Name: "hostile", Codec: hostileRegistryCodec{}}); !errors.Is(err, fault.Panicked) {
		t.Fatal("registration panic identity did not reach application hook")
	}
	if registry.RegisterDefaultPgType(registryValue{}, "int8") == nil {
		t.Fatal("registration panic did not latch failure")
	}
}

func TestTypeRegistryClonedOwnedCodecRetainsAliasDescriptor(t *testing.T) {
	m, registry := testTypeRegistry()
	original := &pgtype.Type{OID: 81008, Name: "owned_original", Codec: registryCodec{label: "owned"}}
	if err := registry.RegisterType(original); err != nil {
		t.Fatal("register original failed")
	}
	owned, _ := m.TypeForName(original.Name)
	alias := &pgtype.Type{OID: 81009, Name: "owned_alias", Codec: owned.Codec}
	if err := registry.RegisterType(alias); err != nil {
		t.Fatal("register borrowed codec alias failed")
	}
	registered, _ := m.TypeForName(alias.Name)
	if registered == alias || registered == owned {
		t.Fatal("borrowed codec skipped descriptor snapshot")
	}
	codec := registered.Codec.(protectedCodec)
	if codec.descriptor != registered {
		t.Fatal("alias retained another descriptor's identity")
	}
	if _, nested := codec.native.(protectedCodec); nested {
		t.Fatal("codec was wrapped twice")
	}
	if err := registry.RegisterDefaultPgType(registryValue{}, alias.Name); err != nil {
		t.Fatal("map alias failed")
	}
	encoded, err := m.Encode(0, pgtype.BinaryFormatCode, protectValue(m, registryValue{}), nil)
	if err != nil || string(encoded) != "owned" {
		t.Fatal("borrowed codec alias lost inference")
	}
}

func TestTypeRegistryRepeatedDescriptorIsIdempotent(t *testing.T) {
	m, registry := testTypeRegistry()
	original := &pgtype.Type{OID: 81010, Name: "idempotent", Codec: registryCodec{label: "one"}}
	if err := registry.RegisterType(original); err != nil {
		t.Fatal("register original failed")
	}
	owned, _ := m.TypeForName(original.Name)
	before := m.PlanEncode(0, pgtype.TextFormatCode, registrationProbe{})
	if err := registry.RegisterType(original); err != nil {
		t.Fatal("repeat registration failed")
	}
	after := m.PlanEncode(0, pgtype.TextFormatCode, registrationProbe{})
	repeated, _ := m.TypeForName(original.Name)
	if before != after || owned != repeated {
		t.Fatal("identical registration replaced descriptor or invalidated cached plans")
	}
	alias := &pgtype.Type{OID: original.OID, Name: "later_alias", Codec: registryCodec{label: "two"}}
	if err := registry.RegisterType(alias); err != nil {
		t.Fatal("register alias failed")
	}
	if err := registry.RegisterType(original); err != nil {
		t.Fatal("restore original failed")
	}
	current, _ := m.TypeForOID(original.OID)
	if current != owned {
		t.Fatal("idempotence ignored registration order after alias replacement")
	}
}
