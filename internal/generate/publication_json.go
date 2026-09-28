package generate

import (
	"bytes"
	"encoding/json"

	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
)

// Ownership documents share the framework's duplicate-key/Unicode/depth parser.
// The second typed decode rejects unknown fields without normalizing stored bytes.
func decodePublicationJSON(data []byte, target any) error {
	if _, err := jsonwire.Decode(data, jsonwire.Limits{Bytes: 8 << 20, Depth: 16, Nodes: 1 << 20}); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}
