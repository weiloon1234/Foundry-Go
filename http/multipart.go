package http

import (
	"context"
	"path/filepath"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/httpquery"
)

// Multipart binds declared form parts to one concrete request DTO. Text parts
// reuse typed URL scalar codecs; JSON parts reuse explicit JSON contracts.
// File parts retain request-owned UploadedFile handles. No model becomes a DTO.
// The zero descriptor is invalid. Generated forms use these same declarations.
type Multipart[B any] struct {
	parts     []MultipartPart[B]
	indexes   map[string]int
	text      Query[B]
	directory string
	err       error
}

// DefineMultipart snapshots exact case-sensitive wire names. Repeated parts
// use the same literal name; brackets and dots do not imply nested objects.
func DefineMultipart[B any](parts ...MultipartPart[B]) Multipart[B] {
	d := Multipart[B]{parts: slices.Clone(parts), indexes: make(map[string]int, len(parts))}
	slices.SortFunc(d.parts, func(a, b MultipartPart[B]) int { return strings.Compare(a.info.Name, b.info.Name) })
	var text []QueryParameter[B]
	for i, part := range d.parts {
		if part.err != nil {
			d.err = part.err
			break
		}
		_, duplicate := d.indexes[part.info.Name]
		if !httpquery.ValidName(part.info.Name) || duplicate || part.kind == "" || part.kind != MultipartText && part.assign == nil {
			d.err = fault.New(fault.Invalid, "multipart part requires a unique name, type and field selector")
			break
		}
		if part.kind == MultipartText {
			text = append(text, part.text)
		}
		d.indexes[part.info.Name] = i
	}
	d.text = DefineQuery(text...)
	return d
}

// WithTempDirectory configures a server-owned absolute directory. Empty uses
// the operating system's temporary directory. Assembly performs no file I/O;
// each request lazily creates a private directory inside this configured root.
func (d Multipart[B]) WithTempDirectory(directory string) Multipart[B] {
	d.directory = directory
	return d
}

func (d Multipart[B]) Validate() error {
	if d.err != nil {
		return d.err
	}
	if d.indexes == nil {
		return fault.New(fault.Invalid, "multipart descriptor is not defined")
	}
	if strings.IndexByte(d.directory, 0) >= 0 || d.directory != "" && !filepath.IsAbs(d.directory) {
		return fault.New(fault.Invalid, "upload temporary directory must be an absolute server path")
	}
	return d.text.Validate()
}

// Description returns owned metadata from the codecs used for actual decoding.
// Files are explicit multipart values and do not acquire a JSON model schema.
func (d Multipart[B]) Description() (MultipartInfo, error) {
	if err := d.Validate(); err != nil {
		return MultipartInfo{}, err
	}
	info := MultipartInfo{Parts: make([]MultipartPartInfo, len(d.parts))}
	text, err := d.text.Parameters()
	if err != nil {
		return MultipartInfo{}, err
	}
	textIndex := 0
	for i, part := range d.parts {
		info.Parts[i] = MultipartPartInfo{QueryParameterInfo: part.info, Kind: part.kind}
		if part.kind == MultipartText {
			info.Parts[i].QueryParameterInfo = text[textIndex]
			textIndex++
		}
		if part.schema != nil {
			schema, err := part.schema()
			if err != nil {
				return MultipartInfo{}, err
			}
			info.Parts[i].Schema = &schema
		}
	}
	return info, nil
}

// MultipartKind identifies each declared part's transport representation.
type MultipartKind string

const (
	MultipartText MultipartKind = "text"
	MultipartFile MultipartKind = "file"
	MultipartJSON MultipartKind = "json"
)

// MultipartPartInfo shares scalar/cardinality metadata with query bindings.
// Scalar and DefaultURL apply only to text parts; JSON parts supply Schema.
// A file has neither a scalar codec nor a JSON schema.
type MultipartPartInfo struct {
	QueryParameterInfo
	Kind   MultipartKind    `json:"kind"`
	Schema *contract.Schema `json:"schema,omitempty"`
}
type MultipartInfo struct {
	Parts []MultipartPartInfo `json:"parts"`
}

// MultipartError reports bounded field diagnostics without submitted names,
// filenames, values or reader errors in its public text. Paths are JSON Pointers
// over declared form names, including repeated-part indexes where applicable.
type MultipartError struct {
	issues []contract.Issue
	cause  error
}

func (*MultipartError) Error() string              { return "invalid multipart form" }
func (e *MultipartError) GoString() string         { return e.Error() }
func (*MultipartError) Is(target error) bool       { return target == fault.Invalid }
func (e *MultipartError) Unwrap() error            { return e.cause }
func (e *MultipartError) Issues() []contract.Issue { return slices.Clone(e.issues) }

func multipartIssue(path string, code contract.IssueCode) error {
	return &MultipartError{issues: []contract.Issue{{Path: path, Code: code}}}
}

func (d Multipart[B]) bind(ctx context.Context, input multipartInput, limits EndpointLimits) (B, error) {
	var issues []contract.Issue
	for i, part := range d.parts {
		if code := parameterCardinalityIssue(part.info, input.counts[i]); code != "" && len(issues) < limits.Multipart.Issues {
			issues = append(issues, contract.Issue{Path: queryIssuePath(part.info.Name), Code: code})
		}
	}
	if len(issues) != 0 {
		return *new(B), &MultipartError{issues: issues}
	}
	result, err := d.text.decodeValues(ctx, input.text, limits.Multipart.Issues)
	if err != nil {
		if failure, ok := err.(*QueryError); ok {
			return *new(B), &MultipartError{issues: failure.Issues(), cause: failure}
		}
		return *new(B), err
	}
	var returned error
	jsonLimits := limits.Body
	jsonLimits.Issues = min(jsonLimits.Issues, limits.Multipart.Issues)
	owned := callback.Isolated("decode multipart fields", func() error {
		for _, part := range d.parts {
			if ctx.Err() != nil {
				break
			}
			if part.assign != nil {
				returned = part.assign(ctx, &result, input.values[part.info.Name], jsonLimits)
				if returned != nil {
					break
				}
			}
		}
		return nil
	})
	if owned != nil {
		return *new(B), fault.Wrap(fault.Internal, "multipart field callback failed", owned)
	}
	if err := ctx.Err(); err != nil {
		return *new(B), err
	}
	if returned != nil {
		return *new(B), returned
	}
	return result, nil
}
