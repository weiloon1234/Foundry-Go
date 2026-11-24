package orm

import (
	"reflect"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm/internal/declarationsnapshot"
)

// These declaration-only records are removed with foundry models convert.
type conversionTableMetadata struct {
	snapshotHooks, snapshotScopes, snapshotRetrieval string
	snapshotNoTimestamps, snapshotNoSoftDeletes      bool
}
type conversionOptionMetadata struct{ snapshotHooks, snapshotScopes, snapshotRetrieval string }
type conversionColumnMetadata struct {
	snapshotCodec                         []declarationsnapshot.Function
	snapshotMutator, snapshotNamedMutator string
}

func recordConversionOption(core *tableCore, option Option) {
	if option.snapshotHooks != "" {
		core.snapshotHooks = option.snapshotHooks
	}
	if option.snapshotScopes != "" {
		core.snapshotScopes = option.snapshotScopes
	}
	if option.snapshotRetrieval != "" {
		core.snapshotRetrieval = option.snapshotRetrieval
	}
}
func recordConversionSettings(d *declaration) {
	d.core.snapshotNoTimestamps = d.noTimestamps
	d.core.snapshotNoSoftDeletes = d.noSoftDeletes
}
func recordConversionCodec[V any](core *columnCore, c codec.Codec[V]) {
	core.snapshotCodec = snapshotCodec(reflect.ValueOf(c))
}
func recordConversionMutator(core *columnCore, fn reflect.Value) {
	core.snapshotMutator = snapshotFunction(fn)
}
func recordConversionNamedMutator(core *columnCore, fn reflect.Value) {
	core.snapshotNamedMutator = snapshotFunction(fn)
}
func conversionMutator(core *columnCore) string {
	if core.snapshotNamedMutator != "" {
		return core.snapshotNamedMutator
	}
	return core.snapshotMutator
}
