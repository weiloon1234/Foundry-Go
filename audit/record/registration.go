package record

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

// Writer is the narrow storage boundary used by generated audit observers. The
// supplied transaction is the real business transaction; returning an error
// fails its model write. Implementations must support concurrent operations.
type Writer interface {
	RecordModel(context.Context, *database.Tx, Entry) error
}

// ResolveWriter binds the writer during ordinary application construction,
// before the database starts. Generated callbacks retain it without a runtime
// service lookup or second application container.
type ResolveWriter func(foundation.Resolver) (Writer, error)

// Declaration wraps a generated model's existing observer registration. It is
// opaque outside this explicit declaration boundary and owns no global state.
type Declaration struct {
	install func(*foundation.Registrar, foundation.Key[*database.DB], ResolveWriter) error
}

// Declare is called by generated model adapters. Application providers pass the
// resulting declaration to audit.Register with their typed database/writer keys.
func Declare(install func(*foundation.Registrar, foundation.Key[*database.DB], ResolveWriter) error) Declaration {
	return Declaration{install: install}
}

// Register invokes the generated registration using foundation/database's
// existing duplicate, dependency, construction and frozen-state handling.
func (d Declaration) Register(r *foundation.Registrar, pool foundation.Key[*database.DB], resolve ResolveWriter) error {
	if d.install == nil || resolve == nil {
		return fault.New(fault.Invalid, "audit registration requires a model declaration and writer")
	}
	return d.install(r, pool, resolve)
}

// ObserverName derives one stable identifier per concrete model for the existing
// database-wide observer namespace. It is not a durable model/subject identity.
// Hashing avoids invalid package-path characters and identifier truncation.
func ObserverName[M any]() (string, error) {
	typ := reflect.TypeFor[M]()
	if typ.Kind() != reflect.Struct || typ.Name() == "" || typ.PkgPath() == "" {
		return "", fault.New(fault.Invalid, "audit observer requires a defined model struct")
	}
	digest := sha256.Sum256([]byte(typ.PkgPath() + "." + typ.Name()))
	return "foundry.audit." + hex.EncodeToString(digest[:]), nil
}
