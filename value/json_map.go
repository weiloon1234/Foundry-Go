package value

import (
	"context"
	"encoding/json"
	"reflect"
	"strconv"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/internal/jsonshape"
)

// Storage snapshots retain their own bounds. HTTP callers use CheckJSONKeys
// with transport bounds; both paths share native parsing and canonicalization.
func validateJSONMapKey(key string, typ reflect.Type) error {
	_, err := parseJSONMapKey(context.Background(), key, typ, JSONMaxBytes)
	return err
}

func parseJSONMapKey(ctx context.Context, key string, typ reflect.Type, outputLimit int) (reflect.Value, error) {
	supported, custom := jsonshape.MapKeyCapabilities(typ)
	if ctx == nil || !supported || outputLimit <= 0 || !utf8.ValidString(key) {
		return reflect.Value{}, invalidJSON()
	}
	if err := ctx.Err(); err != nil {
		return reflect.Value{}, err
	}
	if custom {
		return parseCustomJSONMapKey(ctx, key, typ, outputLimit)
	}
	decoded := reflect.New(typ).Elem()
	var canonical string
	switch typ.Kind() {
	case reflect.String:
		decoded.SetString(key)
		return decoded, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(key, 10, typ.Bits())
		if err != nil {
			return reflect.Value{}, invalidJSON()
		}
		canonical = strconv.FormatInt(n, 10)
		decoded.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(key, 10, typ.Bits())
		if err != nil {
			return reflect.Value{}, invalidJSON()
		}
		canonical = strconv.FormatUint(n, 10)
		decoded.SetUint(n)
	default:
		return reflect.Value{}, invalidJSON()
	}
	if canonical != key {
		return reflect.Value{}, invalidJSON()
	}
	return decoded, nil
}

// Probe a one-entry map using the same native encoder as framework JSON output.
// Payload values are empty structs; no application value methods are evaluated.
// The caller owns callback isolation and keeps resources until methods return.
func parseCustomJSONMapKey(ctx context.Context, key string, typ reflect.Type, outputLimit int) (reflect.Value, error) {
	data, err := json.Marshal(map[string]struct{}{key: {}})
	if err != nil {
		return reflect.Value{}, err
	}
	destination := reflect.New(reflect.MapOf(typ, reflect.TypeFor[struct{}]()))
	if err := json.Unmarshal(data, destination.Interface()); err != nil {
		return reflect.Value{}, err
	}
	if destination.Elem().Len() != 1 {
		return reflect.Value{}, invalidJSON()
	}
	iter := destination.Elem().MapRange()
	iter.Next()
	decoded := iter.Key()
	if !decoded.Comparable() || !decoded.Equal(decoded) {
		return reflect.Value{}, invalidJSON()
	}
	encoded, err := EncodeJSON(ctx, destination.Elem().Interface(), JSONEncodingLimits{
		Bytes: outputLimit, Depth: 1, Nodes: 3, Steps: 8,
	})
	if err != nil {
		return reflect.Value{}, err
	}
	var canonical map[string]struct{}
	if err := json.Unmarshal(encoded, &canonical); err != nil || len(canonical) != 1 {
		return reflect.Value{}, invalidJSON()
	}
	if _, ok := canonical[key]; !ok {
		return reflect.Value{}, invalidJSON()
	}
	return decoded, nil
}
