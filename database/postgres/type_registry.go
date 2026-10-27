package postgres

import (
	"context"
	pgx "github.com/jackc/pgx/v5"
	"strconv"
	"sync"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/internal/sqlcallback"
)

// TypeRegistry configures one physical connection during Config.RegisterTypes.
// Methods serialize registration and fail after that callback returns. Registered
// codecs and their application-owned state must remain immutable afterward.
type TypeRegistry struct {
	mu      sync.Mutex
	conn    *pgx.Conn
	owner   *codecRegistry
	closed  bool
	failure error
}

// RegisterType snapshots the descriptor and native container descriptors, preserving
// pgx's registration order and name/OID alias semantics. The codec itself is retained.
func (r *TypeRegistry) RegisterType(typ *pgtype.Type) error {
	return r.apply("register PostgreSQL type", func() error {
		if typ == nil || typ.Codec == nil || typ.OID == 0 || typ.Name == "" || typ.Name == protectedArgumentType {
			return safeerror.New("invalid PostgreSQL type registration")
		}
		r.owner.register(typ)
		return nil
	})
}

// RegisterDefaultPgType maps exactly the Go type of value to a PostgreSQL type
// name, as pgx does. Register pointer, slice or array forms explicitly if needed.
func (r *TypeRegistry) RegisterDefaultPgType(value any, name string) error {
	return r.apply("register PostgreSQL Go type", func() error {
		if name == protectedArgumentType {
			return safeerror.New("reserved PostgreSQL type mapping")
		}
		r.owner.m.RegisterDefaultPgType(value, name)
		return nil
	})
}

// LoadType discovers a server type during connection setup. Register its returned
// descriptor explicitly; load/register dependency types first as required by pgx.
func (r *TypeRegistry) LoadType(ctx context.Context, name string) (typ *pgtype.Type, err error) {
	err = r.apply("load PostgreSQL type", func() error {
		if r.conn == nil {
			return safeerror.New("PostgreSQL type loading requires a connection")
		}
		var failure error
		typ, failure = loadTypeInCatalog(ctx, r.conn, name)
		return failure
	})
	return
}

// Native pgx discovery uses unqualified catalog/type names. Resolve the caller's
// target first, then restrict only this setup operation to the system catalog.
func loadTypeInCatalog(ctx context.Context, conn *pgx.Conn, name string) (typ *pgtype.Type, err error) {
	resolved := conn.PgConn().ExecParams(ctx,
		"SELECT pg_catalog.current_setting('search_path'), $1::pg_catalog.text::pg_catalog.regtype::pg_catalog.oid::pg_catalog.text",
		[][]byte{[]byte(name)}, []uint32{pgtype.TextOID}, nil, nil).Read()
	if resolved.Err != nil {
		return nil, resolved.Err
	}
	if len(resolved.Rows) != 1 || len(resolved.Rows[0]) != 2 || resolved.Rows[0][0] == nil || resolved.Rows[0][1] == nil {
		return nil, safeerror.New("invalid PostgreSQL type discovery result")
	}
	originalPath := string(resolved.Rows[0][0])
	oid, parseErr := strconv.ParseUint(string(resolved.Rows[0][1]), 10, 32)
	if parseErr != nil || oid == 0 {
		return nil, safeerror.New("invalid PostgreSQL type discovery identity")
	}
	defer func() {
		if restoreErr := setTypeDiscoveryPath(ctx, conn, originalPath); restoreErr != nil {
			err = safeerror.Join("operation failed", err, restoreErr)
		}
		if err != nil {
			typ = nil
		}
	}()
	if err = setTypeDiscoveryPath(ctx, conn, "pg_catalog, pg_temp"); err != nil {
		return nil, err
	}
	typ, err = conn.LoadType(ctx, strconv.FormatUint(oid, 10))
	if err == nil {
		typ.Name = name
	}
	return
}
func setTypeDiscoveryPath(ctx context.Context, conn *pgx.Conn, path string) error {
	result := conn.PgConn().ExecParams(ctx,
		"SELECT pg_catalog.set_config('search_path', $1::pg_catalog.text, false)",
		[][]byte{[]byte(path)}, []uint32{pgtype.TextOID}, nil, nil).Read()
	if result.Err != nil {
		return result.Err
	}
	if len(result.Rows) != 1 || len(result.Rows[0]) != 1 || string(result.Rows[0][0]) != path {
		return safeerror.New("cannot establish PostgreSQL type discovery scope")
	}
	return nil
}

func (r *TypeRegistry) apply(operation string, fn func() error) error {
	if r == nil {
		return safeerror.New("PostgreSQL type registry is unavailable")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return safeerror.New("PostgreSQL type registry is closed")
	}
	if r.failure != nil {
		return r.failure
	}
	if r.owner == nil {
		r.failure = safeerror.New("PostgreSQL type registry is unavailable")
		return r.failure
	}
	r.failure = safeerror.Reveal(operation+" failed", sqlcallback.Invoke(operation, fn))
	return r.failure
}

func initializeTypeRegistry(ctx context.Context, conn *pgx.Conn, configure func(context.Context, *TypeRegistry) error) error {
	var registry *TypeRegistry
	err := sqlcallback.Invoke("initialize PostgreSQL types", func() error {
		protectCodecMap(conn.TypeMap())
		registry = &TypeRegistry{conn: conn, owner: codecRegistryFor(conn.TypeMap())}
		if configure != nil {
			return configure(ctx, registry)
		}
		return nil
	})
	if registry != nil {
		registry.mu.Lock()
		registry.closed = true
		registry.conn = nil
		registry.owner = nil
		failure := registry.failure
		registry.mu.Unlock()
		if err == nil {
			err = safeerror.Seal("initialize PostgreSQL types failed", failure)
		}
	}
	return err
}

type codecRegistry struct {
	m           *pgtype.Map
	descriptors map[*pgtype.Type]*pgtype.Type
}

func codecRegistryFor(m *pgtype.Map) *codecRegistry {
	if typ, ok := m.TypeForOID(0); ok && typ.Name == protectedArgumentType {
		if codec, ok := typ.Codec.(protectedCodec); ok && codec.registry != nil && codec.native == nil {
			return codec.registry
		}
	}
	registry := &codecRegistry{m: m, descriptors: make(map[*pgtype.Type]*pgtype.Type)}
	typ := &pgtype.Type{Name: protectedArgumentType}
	typ.Codec = protectedCodec{registry: registry, descriptor: typ}
	m.RegisterType(typ)
	return registry
}

func (r *codecRegistry) register(typ *pgtype.Type) {
	owned := r.decorate(typ)
	currentOID, _ := r.m.TypeForOID(owned.OID)
	currentName, _ := r.m.TypeForName(owned.Name)
	if currentOID != owned || currentName != owned {
		r.m.RegisterType(owned)
	}
}

func (r *codecRegistry) decorate(typ *pgtype.Type) *pgtype.Type {
	if typ == nil {
		return nil
	}
	if codec, ok := typ.Codec.(protectedCodec); ok && codec.registry == r && codec.descriptor == typ {
		return typ
	}
	if owned := r.descriptors[typ]; owned != nil {
		return owned
	}
	copied := &pgtype.Type{Name: typ.Name, OID: typ.OID}
	r.descriptors[typ] = copied
	native := typ.Codec
	switch codec := native.(type) {
	case protectedCodec:
		native = codec.native
	}
	switch codec := native.(type) {
	case *pgtype.ArrayCodec:
		clone := *codec
		clone.ElementType = r.decorate(codec.ElementType)
		native = &clone
	case *pgtype.RangeCodec:
		clone := *codec
		clone.ElementType = r.decorate(codec.ElementType)
		native = &clone
	case *pgtype.MultirangeCodec:
		clone := *codec
		clone.ElementType = r.decorate(codec.ElementType)
		native = &clone
	case *pgtype.CompositeCodec:
		clone := *codec
		clone.Fields = append([]pgtype.CompositeCodecField(nil), codec.Fields...)
		for i := range clone.Fields {
			clone.Fields[i].Type = r.decorate(codec.Fields[i].Type)
		}
		native = &clone
	case *pgtype.JSONCodec:
		native = &pgtype.JSONCodec{Marshal: protectedJSONMarshal(codec.Marshal), Unmarshal: protectedJSONUnmarshal(codec.Unmarshal)}
	case *pgtype.JSONBCodec:
		native = &pgtype.JSONBCodec{Marshal: protectedJSONMarshal(codec.Marshal), Unmarshal: protectedJSONUnmarshal(codec.Unmarshal)}
	}
	copied.Codec = protectedCodec{native: native, registry: r, descriptor: copied}
	return copied
}
