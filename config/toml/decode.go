// Package toml adapts bounded TOML documents to Foundry's typed configuration
// schema. It does not discover settings through TOML struct tags or load files
// implicitly. The caller owns the reader and applies the returned file layer.
package toml

import (
	"io"
	"math"
	"sort"
	"strings"
	"time"

	parser "github.com/pelletier/go-toml/v2"
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// DefaultMaxBytes bounds an input document when Options.MaxBytes is zero.
const DefaultMaxBytes int64 = 1 << 20

const maxDepth = 64

// Options names a trusted source label for provenance and bounds bytes read.
// Do not put credentials or the document contents in Name. MaxBytes must be
// positive or zero for DefaultMaxBytes; the decoder reads at most one extra
// byte to detect an oversized document. Reader deadlines belong to the caller.
type Options struct {
	Name     string
	MaxBytes int64
}

// Decode reads one TOML document, resolving namespaces against schema. Scalars
// become text; arrays and tables owned by a declared key become JSON for a
// config.JSON or custom key decoder. Dates/times retain ISO text without an
// implicit timezone. Non-finite floats and excessive nesting are rejected.
//
// The result is a file layer, not validated settings: Schema.Load still applies
// typed field decoders and validation. Errors return an empty layer. Error
// formatting omits input values; unwrap-accessible parser/I/O causes may contain
// sensitive input and must not be logged. Decode does not close the reader.
func Decode[T any](reader io.Reader, schema *config.Schema[T], options Options) (config.Values, error) {
	if reader == nil || schema == nil || strings.TrimSpace(options.Name) == "" {
		return config.Values{}, fault.New(fault.Invalid, "TOML configuration needs a reader, schema and source name")
	}
	limit := options.MaxBytes
	if limit == 0 {
		limit = DefaultMaxBytes
	}
	if limit < 0 || limit == math.MaxInt64 {
		return config.Values{}, fault.New(fault.Invalid, "invalid TOML configuration byte limit")
	}
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return config.Values{}, fault.Wrap(fault.Invalid, "cannot read TOML configuration from "+options.Name, err)
	}
	if int64(len(data)) > limit {
		return config.Values{}, fault.New(fault.Invalid, "TOML configuration exceeds byte limit in "+options.Name)
	}
	var document map[string]any
	if err := parser.Unmarshal(data, &document); err != nil {
		return config.Values{}, fault.Wrap(fault.Invalid, "cannot parse TOML configuration from "+options.Name, err)
	}
	normalized, err := normalize(document, 0)
	if err != nil {
		return config.Values{}, fault.Wrap(fault.Invalid, "invalid TOML configuration in "+options.Name, err)
	}
	values, err := config.DecodeObject(normalized.(map[string]any), schema, options.Name)
	if err != nil {
		return config.Values{}, fault.Wrap(fault.Invalid, "invalid TOML configuration in "+options.Name, err)
	}
	return values, nil
}

func normalize(value any, depth int) (any, error) {
	if depth > maxDepth {
		return nil, fault.New(fault.Invalid, "TOML configuration nesting exceeds limit")
	}
	switch value := value.(type) {
	case string, int64, bool:
		return value, nil
	case float64:
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, fault.New(fault.Invalid, "TOML configuration requires finite numbers")
		}
		return value, nil
	case time.Time:
		return value.Format(time.RFC3339Nano), nil
	case parser.LocalDate:
		return value.String(), nil
	case parser.LocalTime:
		return value.String(), nil
	case parser.LocalDateTime:
		return value.String(), nil
	case []any:
		for i, child := range value {
			converted, err := normalize(child, depth+1)
			if err != nil {
				return nil, err
			}
			value[i] = converted
		}
		return value, nil
	case map[string]any:
		for _, key := range sortedKeys(value) {
			converted, err := normalize(value[key], depth+1)
			if err != nil {
				return nil, err
			}
			value[key] = converted
		}
		return value, nil
	default:
		return nil, fault.New(fault.Invalid, "unsupported TOML configuration value")
	}
}

func sortedKeys(table map[string]any) []string {
	keys := make([]string, 0, len(table))
	for key := range table {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
