package http

import (
	"context"
	"strconv"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// MultipartPart declares one typed text, JSON or file field. Selectors must
// select only their field on the supplied DTO, without retaining or sharing it.
// All selector execution is owned by the request until it actually finishes.
type MultipartPart[B any] struct {
	info   QueryParameterInfo
	kind   MultipartKind
	text   QueryParameter[B]
	schema func() (contract.Schema, error)
	assign func(context.Context, *B, []multipartValue, contract.JSONLimits) error
	err    error
}

type multipartValue struct {
	file UploadedFile
	data []byte
}

// TextPart reuses an ordinary required, optional, repeated or default query
// parameter's codec and field selector. Raw form text is not URL-unescaped.
func TextPart[B any](parameter QueryParameter[B]) MultipartPart[B] {
	return MultipartPart[B]{info: parameter.info, kind: MultipartText, text: parameter, err: parameter.err}
}

// FilePart requires one file part, including an explicitly uploaded empty file.
// Use file validation to require nonempty contents; presence is independent.
func FilePart[B any](name string, field func(*B) *UploadedFile) MultipartPart[B] {
	info := QueryParameterInfo{Name: name, Required: true}
	if field == nil {
		return MultipartPart[B]{info: info, kind: MultipartFile}
	}
	return bindFilePart(info, func(body *B, files []UploadedFile) bool {
		if target := field(body); target != nil {
			*target = files[0]
			return true
		}
		return false
	})
}

// OptionalFilePart distinguishes omission from a supplied zero-byte file.
func OptionalFilePart[B any](name string, field func(*B) *value.Optional[UploadedFile]) MultipartPart[B] {
	info := QueryParameterInfo{Name: name}
	if field == nil {
		return MultipartPart[B]{info: info, kind: MultipartFile}
	}
	return bindFilePart(info, func(body *B, files []UploadedFile) bool {
		if target := field(body); target != nil {
			*target = value.Optional[UploadedFile]{}
			if len(files) != 0 {
				*target = value.Set(files[0])
			}
			return true
		}
		return false
	})
}

// RepeatedFilePart preserves request order in an ordinary or named slice.
// An omitted field produces nil; a present empty file is one element.
func RepeatedFilePart[B any, S ~[]UploadedFile](name string, field func(*B) *S) MultipartPart[B] {
	info := QueryParameterInfo{Name: name, Repeated: true}
	if field == nil {
		return MultipartPart[B]{info: info, kind: MultipartFile}
	}
	return bindFilePart(info, func(body *B, files []UploadedFile) bool {
		if target := field(body); target != nil {
			*target = S(files)
			return true
		}
		return false
	})
}

func bindFilePart[B any](info QueryParameterInfo, write func(*B, []UploadedFile) bool) MultipartPart[B] {
	return bindMultipartValues(info, MultipartFile, func(_ context.Context, item multipartValue, _ contract.JSONLimits) (UploadedFile, error) {
		return item.file, nil
	}, write)
}

// JSONPart requires one application/json part decoded by its declared contract.
// JSON nullability belongs to that concrete value's descriptor, independently
// of whether the multipart field is required or optional.
func JSONPart[B, V any](name string, descriptor contract.JSON[V], field func(*B) *V) MultipartPart[B] {
	info := QueryParameterInfo{Name: name, Required: true}
	if field == nil {
		return MultipartPart[B]{info: info, kind: MultipartJSON}
	}
	return bindJSONPart(info, descriptor, func(body *B, items []V) bool {
		if target := field(body); target != nil {
			*target = items[0]
			return true
		}
		return false
	})
}

// OptionalJSONPart preserves omission separately from a present JSON value,
// including explicit null when V's declared JSON contract supports it.
func OptionalJSONPart[B, V any](name string, descriptor contract.JSON[V], field func(*B) *value.Optional[V]) MultipartPart[B] {
	info := QueryParameterInfo{Name: name}
	if field == nil {
		return MultipartPart[B]{info: info, kind: MultipartJSON}
	}
	return bindJSONPart(info, descriptor, func(body *B, items []V) bool {
		if target := field(body); target != nil {
			*target = value.Optional[V]{}
			if len(items) != 0 {
				*target = value.Set(items[0])
			}
			return true
		}
		return false
	})
}

// RepeatedJSONPart decodes each same-named JSON part into one slice element.
func RepeatedJSONPart[B, V any, S ~[]V](name string, descriptor contract.JSON[V], field func(*B) *S) MultipartPart[B] {
	info := QueryParameterInfo{Name: name, Repeated: true}
	if field == nil {
		return MultipartPart[B]{info: info, kind: MultipartJSON}
	}
	return bindJSONPart(info, descriptor, func(body *B, items []V) bool {
		if target := field(body); target != nil {
			*target = S(items)
			return true
		}
		return false
	})
}

func bindJSONPart[B, V any](info QueryParameterInfo, descriptor contract.JSON[V], write func(*B, []V) bool) MultipartPart[B] {
	part := bindMultipartValues(info, MultipartJSON, func(ctx context.Context, item multipartValue, limits contract.JSONLimits) (V, error) {
		return descriptor.Decode(ctx, item.data, limits)
	}, write)
	part.err = descriptor.Validate()
	part.schema = descriptor.Description
	return part
}

func bindMultipartValues[B, V any](info QueryParameterInfo, kind MultipartKind, decode func(context.Context, multipartValue, contract.JSONLimits) (V, error), write func(*B, []V) bool) MultipartPart[B] {
	return MultipartPart[B]{info: info, kind: kind, assign: func(ctx context.Context, body *B, parts []multipartValue, limits contract.JSONLimits) error {
		var values []V
		if len(parts) != 0 {
			values = make([]V, len(parts))
		}
		for i, part := range parts {
			if err := ctx.Err(); err != nil {
				return err
			}
			item, err := decode(ctx, part, limits)
			if err != nil {
				if failure, ok := err.(*contract.DecodeError); ok {
					issues := failure.Issues()
					prefix := queryIssuePath(info.Name)
					if info.Repeated {
						prefix += "/" + strconv.Itoa(i)
					}
					for j := range issues {
						issues[j].Path = prefix + issues[j].Path
					}
					return &MultipartError{issues: issues, cause: failure}
				}
				return err
			}
			values[i] = item
		}
		if !write(body, values) {
			return fault.New(fault.Internal, "multipart field selector returned nil")
		}
		return nil
	}}
}
