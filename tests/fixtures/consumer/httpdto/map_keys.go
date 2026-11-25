package httpdto

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"foundry.test/consumer/models"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/model"
)

// doc-example:begin warehouse-json-key
// WarehouseKey has a private representation and a canonical native text key.
type WarehouseKey struct{ number uint16 }

func NewWarehouseKey(number uint16) WarehouseKey { return WarehouseKey{number: number} }
func (k WarehouseKey) MarshalText() ([]byte, error) {
	return []byte("warehouse_" + strconv.FormatUint(uint64(k.number), 10)), nil
}
func (k *WarehouseKey) UnmarshalText(data []byte) error {
	text, ok := strings.CutPrefix(string(data), "warehouse_")
	if !ok {
		return fmt.Errorf("invalid warehouse key")
	}
	n, err := strconv.ParseUint(text, 10, 16)
	if err != nil {
		return err
	}
	*k = NewWarehouseKey(uint16(n))
	return nil
}

// JSONKeyContract describes the native key without exposing its stored fields.
func (WarehouseKey) JSONKeyContract() contract.JSONKey[WarehouseKey] {
	typ := reflect.TypeFor[WarehouseKey]()
	return contract.DefineJSONKey(contract.DefineScalar[WarehouseKey](contract.Type{
		ID: contract.TypeID(typ.PkgPath() + "." + typ.Name()), Kind: contract.StringKind,
	}))
}

// doc-example:end warehouse-json-key

func (k WarehouseKey) Number() uint16 { return k.number }

// doc-example:begin inventory-map-keys

//foundry:dto
type InventoryResponse struct {
	ByWarehouse map[WarehouseKey]string
	ByUser      map[model.ID[models.User]]string
	ByStatus    map[models.Status]int
	ByNumber    map[int16]string
}

// doc-example:end inventory-map-keys

var WarehouseKeys = WarehouseKey{}.JSONKeyContract()

func WarehouseKeyDescription() (contract.JSONKeyInfo, error) {
	return WarehouseKeys.Description()
}
