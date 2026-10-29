package codec

import (
	"database/sql/driver"
	"reflect"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/model"
)

// FoundryID adapts the shared UUID codec after validating the exact model owner.
// Example: FoundryID[User, model.ID[User]](sqlowner.Metadata()).
func FoundryID[M any, K comparable](seal sqlowner.MetadataSeal) Codec[K] {
	if !seal.Valid() || reflect.TypeFor[K]() != reflect.TypeFor[model.ID[M]]() {
		return Codec[K]{}
	}
	return typed(TypeUUID, func(v K) (driver.Value, error) {
		return bindSharedID(unsafe.Pointer(&v))
	}, func(raw any) (v K, err error) {
		err = decodeSharedID(raw, unsafe.Pointer(&v))
		return
	})
}

// The exact-type check at the adapter boundary admits only model.ID values.
// All owners have the same UUID representation; parse before publishing it.
func bindSharedID(p unsafe.Pointer) (driver.Value, error) {
	return (*model.ID[struct{}])(p).String(), nil
}

func decodeSharedID(raw any, p unsafe.Pointer) error {
	source, err := text(raw)
	if err != nil {
		return err
	}
	id, err := model.ParseID[struct{}](source)
	if err == nil {
		*(*model.ID[struct{}])(p) = id
	}
	return err
}
