package record

import (
	"encoding/json"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Document captures a typed domain audit DTO. Sensitive JSON keys use the same
// redaction rules as model fields. Redacted documents remain audit representations;
// they cannot be decoded into partially populated application DTOs.
type Document[P any] struct {
	_         [0]*P
	payload   value.JSON[json.RawMessage]
	redacted  bool
	redaction Redaction
}

func (Document[P]) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("audit document")) }
func (d Document[P]) Redacted() bool               { return d.redacted }
func (d Document[P]) Payload() (string, error)     { return d.payload.Text() }

// Redaction reports the sensitive-name policy the payload satisfies: the policy
// that captured it, or CurrentRedaction when an older document contained keys
// the current policy treats as sensitive and was masked when parsed.
func (d Document[P]) Redaction() Redaction { return d.redaction }

// CaptureDocument freezes an independent representation and isolates custom
// JSON callback faults. Domain DTOs should omit private fields altogether when
// even a redacted marker is inappropriate.
func CaptureDocument[P any](input P) (Document[P], error) {
	result := Document[P]{redaction: CurrentRedaction}
	err := callback.Isolated("capture domain audit", func() error {
		typed, err := value.NewJSON(input)
		if err != nil {
			return err
		}
		text, err := typed.Text()
		if err != nil {
			return err
		}
		text, result.redacted, err = redactJSON(text, CurrentRedaction)
		if err != nil {
			return err
		}
		result.payload, err = value.ParseJSON[json.RawMessage](text)
		return err
	})
	if err != nil {
		return Document[P]{}, fault.Wrap(fault.Invalid, "domain audit capture failed", err)
	}
	return result, nil
}

// ParseDocument checks restored redaction metadata under CurrentRedaction. Use
// ParseDocumentWith for stored history, which records the policy that wrote it.
func ParseDocument[P any](text string, redacted bool) (Document[P], error) {
	return ParseDocumentWith[P](CurrentRedaction, text, redacted)
}

// ParseDocumentWith checks restored redaction metadata under the policy that
// captured the document and applies the concrete DTO schema only to complete,
// unredacted data. It never trusts a persisted flag to hide an unsanitized
// secret value, and extending the current policy never invalidates old rows.
// Keys that CurrentRedaction treats as sensitive in an older document are
// masked on read: the result is redacted and reports CurrentRedaction.
func ParseDocumentWith[P any](policy Redaction, text string, redacted bool) (Document[P], error) {
	if err := policy.Validate(); err != nil {
		return Document[P]{}, err
	}
	clean, changed, err := redactJSON(text, policy)
	if err != nil {
		return Document[P]{}, err
	}
	payload, err := value.ParseJSON[json.RawMessage](text)
	if err != nil {
		return Document[P]{}, err
	}
	canonical, err := payload.Text()
	if err != nil {
		return Document[P]{}, err
	}
	if changed != redacted || clean != canonical {
		return Document[P]{}, fault.New(fault.Invalid, "invalid domain audit redaction metadata")
	}
	result := Document[P]{payload: payload, redacted: redacted, redaction: policy}
	if policy < CurrentRedaction {
		masked, changed, err := redactJSON(canonical, CurrentRedaction)
		if err != nil {
			return Document[P]{}, err
		}
		if changed && masked != canonical {
			if result.payload, err = value.ParseJSON[json.RawMessage](masked); err != nil {
				return Document[P]{}, err
			}
			result.redacted, result.redaction = true, CurrentRedaction
		}
	}
	if !result.redacted {
		if _, err := result.Decode(); err != nil {
			return Document[P]{}, err
		}
	}
	return result, nil
}

// Decode returns a fresh DTO or a Missing error for redacted history. Payload
// remains available for an explicit, already-redacted audit export.
func (d Document[P]) Decode() (P, error) {
	if d.redacted {
		return *new(P), fault.New(fault.Missing, "redacted audit document has no complete DTO")
	}
	text, err := d.payload.Text()
	if err != nil {
		return *new(P), err
	}
	var result P
	err = callback.Isolated("decode domain audit", func() error {
		typed, err := value.ParseJSON[P](text)
		if err != nil {
			return err
		}
		result, err = typed.Decode()
		return err
	})
	if err != nil {
		return *new(P), fault.Wrap(fault.Invalid, "domain audit decoding failed", err)
	}
	return result, nil
}
