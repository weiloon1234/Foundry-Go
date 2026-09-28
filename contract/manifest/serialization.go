package manifest

import (
	"bytes"
	"encoding/json"
	"io"

	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
)

// Decode accepts the supported manifest version and rejects unknown fields,
// duplicate JSON names, invalid Unicode, dangling references and conflicting
// declarations. Data is bounded before decoding or canonical copying.
func Decode(data []byte) (*Manifest, error) {
	if _, err := jsonwire.Decode(data, jsonwire.Limits{Bytes: MaxBytes, Depth: jsonwire.MaxDepth, Nodes: 1 << 20}); err != nil {
		return nil, err
	}
	var document Document
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return nil, invalid("malformed manifest document")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return nil, invalid("trailing manifest data")
	}
	return freeze(document)
}

func freeze(document Document) (*Manifest, error) {
	if err := normalizeDocument(&document); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil || len(data) >= MaxBytes {
		return nil, invalid("manifest exceeds its encoding budget")
	}
	data = append(data, '\n')
	if _, err := jsonwire.Decode(data, jsonwire.Limits{Bytes: MaxBytes, Depth: jsonwire.MaxDepth, Nodes: 1 << 20}); err != nil {
		return nil, invalid("manifest exceeds its wire structure budget")
	}
	return &Manifest{data: data}, nil
}

// JSON returns a complete deterministic owned document, with no timestamps or
// machine-specific paths. A zero Manifest is invalid.
func (m *Manifest) JSON() ([]byte, error) {
	if m == nil || len(m.data) == 0 {
		return nil, invalid("manifest is not initialized")
	}
	return bytes.Clone(m.data), nil
}

func (m *Manifest) Snapshot() (Document, error) {
	if m == nil || len(m.data) == 0 {
		return Document{}, invalid("manifest is not initialized")
	}
	var result Document
	if err := json.Unmarshal(m.data, &result); err != nil {
		return Document{}, invalid("invalid frozen document")
	}
	return result, nil
}
