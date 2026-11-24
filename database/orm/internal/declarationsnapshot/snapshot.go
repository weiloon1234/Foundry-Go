// Package declarationsnapshot owns the serializable declaration comparison schema.
package declarationsnapshot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// Table exists only for foundry models convert; remove it after migration.
type Table struct {
	Name          string     `json:"table"`
	Model         string     `json:"model"`
	Key           string     `json:"key"`
	KeyCodec      []Function `json:"key_codec"`
	Identity      string     `json:"identity"`
	Columns       []Column   `json:"columns"`
	Members       []Member   `json:"members"`
	Slots         []Slot     `json:"slots"`
	Owners        []Owner    `json:"owners"`
	NoTimestamps  bool       `json:"no_timestamps"`
	NoSoftDeletes bool       `json:"no_soft_deletes"`
	Created       string     `json:"created"`
	Updated       string     `json:"updated"`
	Deleted       string     `json:"deleted"`
	Hooks         string     `json:"hooks"`
	Scopes        string     `json:"scopes"`
	Retrieval     string     `json:"retrieval"`
}

// Function exists only for foundry models convert; remove it after migration.
type Function struct {
	Property string `json:"property"`
	Name     string `json:"name"`
}

// Column exists only for foundry models convert; remove it after migration.
type Column struct {
	Field           string     `json:"field"`
	Name            string     `json:"name"`
	Stored          string     `json:"stored"`
	Input           string     `json:"input"`
	Nullable        bool       `json:"nullable"`
	DatabaseDefault bool       `json:"database_default"`
	Role            string     `json:"role"`
	Encrypted       bool       `json:"encrypted"`
	Sensitive       bool       `json:"sensitive"`
	Parameter       string     `json:"parameter"`
	NonCollatable   bool       `json:"non_collatable"`
	Codec           []Function `json:"codec"`
	Mutator         string     `json:"mutator"`
}

// Member exists only for foundry models convert; remove it after migration.
type Member struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Field string `json:"field"`
	Input string `json:"input"`
	Kind  string `json:"kind"`
	Role  string `json:"role"`
}

// Slot exists only for foundry models convert; remove it after migration.
type Slot struct {
	Field     string     `json:"field"`
	Type      string     `json:"type"`
	Target    string     `json:"target"`
	Pivot     string     `json:"pivot"`
	Extension *Extension `json:"extension"`
}

// Encode exists only for foundry models convert; remove it after migration.
func Encode(t Table) ([]byte, error) { return json.MarshalIndent(t, "", "  ") }

// Decode exists only for foundry models convert; remove it after migration.
func Decode(data []byte) (Table, error) {
	var t Table
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&t); err != nil {
		return t, safeerror.Wrap("invalid declaration snapshot", err)
	}
	if err := d.Decode(new(json.RawMessage)); err != io.EOF {
		return t, safeerror.New("trailing declaration snapshot data")
	}
	return t, nil
}

// Compare names every changed leaf, including ordered slice positions.
// Compare exists only for foundry models convert; remove it after migration.
func Compare(before, after Table) []string {
	var differences []string
	compare(reflect.ValueOf(before), reflect.ValueOf(after), "", &differences)
	return differences
}
func compare(a, b reflect.Value, path string, result *[]string) {
	switch a.Kind() {
	case reflect.Pointer:
		if a.IsNil() != b.IsNil() {
			*result = append(*result, path)
			return
		}
		if !a.IsNil() {
			compare(a.Elem(), b.Elem(), path, result)
		}
	case reflect.Struct:
		for i := 0; i < a.NumField(); i++ {
			name := a.Type().Field(i).Tag.Get("json")
			if name == "" {
				name = a.Type().Field(i).Name
			}
			for j, c := range name {
				if c == ',' {
					name = name[:j]
					break
				}
			}
			if path != "" {
				name = path + "." + name
			}
			compare(a.Field(i), b.Field(i), name, result)
		}
	case reflect.Slice:
		if a.Len() != b.Len() {
			*result = append(*result, path+".length")
		}
		for i := 0; i < min(a.Len(), b.Len()); i++ {
			compare(a.Index(i), b.Index(i), fmt.Sprintf("%s[%d]", path, i), result)
		}
	default:
		if !reflect.DeepEqual(a.Interface(), b.Interface()) {
			*result = append(*result, path)
		}
	}
}

// Extension exists only for foundry models convert; remove it after migration.
type Extension struct {
	Field   string `json:"field"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Storage string `json:"storage"`
}

// Owner exists only for foundry models convert; remove it after migration.
type Owner struct {
	Name         string `json:"name"`
	Model        string `json:"model"`
	StorageModel string `json:"storage_model"`
}

// Explain retains the comparison paths and adds field names and both values.
func Explain(before, after Table) []string {
	paths := Compare(before, after)
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		a, b := snapshotLeaf(reflect.ValueOf(before), path), snapshotLeaf(reflect.ValueOf(after), path)
		property := path
		field := ""
		for _, family := range []string{"columns", "members", "slots"} {
			prefix := family + "["
			if strings.HasPrefix(path, prefix) {
				end := strings.IndexByte(path, ']')
				index, _ := strconv.Atoi(path[len(prefix):end])
				switch family {
				case "columns":
					if index < len(before.Columns) {
						field = before.Columns[index].Field
					}
				case "members":
					if index < len(before.Members) {
						field = before.Members[index].Name
					}
				case "slots":
					if index < len(before.Slots) {
						field = before.Slots[index].Field
					}
				}
				property = strings.TrimPrefix(path[end+1:], ".")
			}
		}
		if strings.HasPrefix(property, "codec[") {
			property = "codec"
		}
		property = strings.ReplaceAll(strings.ReplaceAll(property, "_", " "), ".", " ")
		if field != "" {
			property = field + ": " + property
		}
		result = append(result, fmt.Sprintf("%s differs: hand-written %s, generated %s (%s)", property, a, b, path))
	}
	return result
}
func snapshotLeaf(v reflect.Value, path string) string {
	for path != "" && v.IsValid() {
		if v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return "null"
			}
			v = v.Elem()
		}
		if path == "length" {
			return strconv.Itoa(v.Len())
		}
		end := strings.IndexAny(path, ".[")
		if end < 0 {
			end = len(path)
		}
		name := path[:end]
		path = path[end:]
		if name != "" {
			found := reflect.Value{}
			for i := 0; i < v.NumField(); i++ {
				if strings.Split(v.Type().Field(i).Tag.Get("json"), ",")[0] == name {
					found = v.Field(i)
					break
				}
			}
			v = found
		}
		if strings.HasPrefix(path, "[") {
			end = strings.IndexByte(path, ']')
			index, _ := strconv.Atoi(path[1:end])
			if !v.IsValid() || index >= v.Len() {
				return "absent"
			}
			v = v.Index(index)
			path = path[end+1:]
		}
		path = strings.TrimPrefix(path, ".")
	}
	if !v.IsValid() {
		return "absent"
	}
	data, err := json.Marshal(v.Interface())
	if err != nil {
		return "unavailable"
	}
	return string(data)
}
