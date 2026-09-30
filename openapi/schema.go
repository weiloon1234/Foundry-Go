package openapi

import (
	"encoding/json"
	"math/big"
	"strconv"

	"github.com/weiloon1234/Foundry-Go/contract"
)

func (r renderer) schema(typ contract.Type) object {
	result := object{}
	switch typ.Kind {
	case contract.BooleanKind:
		result["type"] = "boolean"
	case contract.StringKind:
		result["type"] = "string"
		switch typ.Format {
		case contract.UUIDFormat:
			result["format"] = "uuid"
		case contract.DateFormat:
			result["format"] = "date"
		case contract.DateTimeFormat:
			result["format"] = "date-time"
		case contract.Base64Format:
			result["contentEncoding"] = "base64"
		}
		if typ.Format != "" {
			result["x-foundry-format"] = typ.Format
		}
	case contract.IntegerKind:
		result["type"] = "integer"
		if typ.Bits <= 16 || typ.Signed && typ.Bits == 32 {
			result["format"] = "int32"
		} else if typ.Signed || typ.Bits <= 32 {
			result["format"] = "int64"
		}
		result["x-foundry-integer-bits"], result["x-foundry-integer-signed"] = typ.Bits, typ.Signed
		bits := uint(typ.Bits)
		if typ.Signed {
			bits--
		}
		bound := new(big.Int).Lsh(big.NewInt(1), bits)
		minimum := "0"
		if typ.Signed {
			minimum = new(big.Int).Neg(new(big.Int).Set(bound)).String()
		}
		result["minimum"], result["maximum"] = json.Number(minimum), json.Number(new(big.Int).Sub(bound, big.NewInt(1)).String())
	case contract.NumberKind:
		result["type"] = "number"
		if typ.Bits == 32 {
			result["format"] = "float"
		}
		if typ.Bits == 64 {
			result["format"] = "double"
		}
		if typ.Bits == 0 {
			result["x-foundry-exact-number"] = true
		}
	case contract.ObjectKind:
		properties := object{}
		required := make([]string, 0)
		for _, p := range typ.Properties {
			properties[p.Name] = presentationSchema(r.ref(p.Type), p.Presentation)
			if p.Required {
				required = append(required, p.Name)
			}
		}
		result["type"], result["properties"], result["additionalProperties"] = "object", properties, false
		if len(required) != 0 {
			result["required"] = required
		}
	case contract.UnionKind:
		oneOf := make([]any, 0, len(typ.Variants))
		mapping := object{}
		for i, variant := range typ.Variants {
			reference := "#/components/schemas/" + r.variants[typ.ID][i]
			oneOf = append(oneOf, object{"$ref": reference})
			mapping[variant.Tag] = reference
		}
		result["oneOf"] = oneOf
		result["discriminator"] = object{"propertyName": typ.Discriminator, "mapping": mapping}
	case contract.ArrayKind:
		result["type"], result["items"] = "array", r.ref(typ.Element)
		if length, fixed := typ.Length.Get(); fixed {
			result["minItems"], result["maxItems"] = length, length
		}
	case contract.MapKind:
		result["type"], result["additionalProperties"] = "object", r.ref(typ.Element)
		if typ.Key != nil {
			result["propertyNames"] = r.key(*typ.Key)
		}
	case contract.AliasKind:
		result = object{"allOf": []any{r.ref(typ.Element)}, "not": object{"type": "null"}}
	case contract.QuotedKind:
		result = object{"type": "string", "contentMediaType": "application/json", "contentSchema": object{"allOf": []any{r.ref(typ.Element)}, "not": object{"type": "null"}}, "x-foundry-quoted": true}
	case contract.DynamicKind:
		if !typ.Nullable {
			result["not"] = object{"type": "null"}
		}
	}
	if len(typ.Cases) != 0 {
		result["enum"] = typ.Cases
	}
	if typ.Nullable && typ.Kind != contract.DynamicKind {
		result = object{"anyOf": []any{result, object{"type": "null"}}}
	}
	result["title"], result["x-foundry-type-id"] = string(typ.ID), string(typ.ID)
	return result
}

func (r renderer) key(key contract.JSONKeyInfo) object {
	result := object{"type": "string", "x-foundry-key": key}
	if key.Value.Kind == contract.IntegerKind {
		pattern := "^(0|[1-9][0-9]*)$"
		if key.Value.Signed {
			pattern = "^(0|-?[1-9][0-9]*)$"
		}
		result["pattern"] = pattern
	}
	if key.Value.Format == contract.UUIDFormat {
		result["format"] = "uuid"
	}
	if len(key.Value.Cases) != 0 {
		values := make([]string, 0, len(key.Value.Cases))
		for _, raw := range key.Value.Cases {
			text := string(raw)
			if key.Value.Kind == contract.StringKind {
				_ = json.Unmarshal(raw, &text)
			}
			values = append(values, text)
		}
		result["enum"] = values
	}
	if key.NonZero {
		result["not"] = object{"const": "00000000-0000-0000-0000-000000000000"}
	}
	if key.Value.Kind == contract.IntegerKind {
		result["maxLength"] = len(strconv.FormatUint(^uint64(0), 10)) + 1
	}
	return result
}

func (r renderer) taggedVariant(union contract.Type, variant contract.Variant) object {
	payload := r.types[variant.Type]
	properties := object{union.Discriminator: object{"type": "string", "const": variant.Tag}}
	required := []string{union.Discriminator}
	for _, property := range payload.Properties {
		properties[property.Name] = presentationSchema(r.ref(property.Type), property.Presentation)
		if property.Required {
			required = append(required, property.Name)
		}
	}
	return object{"type": "object", "properties": properties, "required": required, "additionalProperties": false, "x-foundry-payload-type": string(payload.ID)}
}
